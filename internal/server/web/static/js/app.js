// Theme management
function initTheme() {
    const savedTheme = localStorage.getItem('theme') || 'light';
    document.documentElement.setAttribute('data-theme', savedTheme);
    updateThemeButton(savedTheme);
}

function toggleTheme() {
    const currentTheme = document.documentElement.getAttribute('data-theme');
    const newTheme = currentTheme === 'light' ? 'dark' : 'light';
    
    document.documentElement.setAttribute('data-theme', newTheme);
    localStorage.setItem('theme', newTheme);
    updateThemeButton(newTheme);
}

function updateThemeButton(theme) {
    const button = document.getElementById('theme-toggle');
    button.textContent = theme === 'light' ? '🌙' : '☀️';
}

// Modal management
function showAddJobModal() {
    document.getElementById('modal-title').textContent = 'Add Cron Job';
    document.getElementById('job-form').reset();
    document.getElementById('job-id').value = '';
    document.getElementById('job-modal').style.display = 'block';
}

function editJob(jobId) {
    fetch(`/api/jobs/${jobId}`)
        .then(response => response.json())
        .then(job => {
            document.getElementById('modal-title').textContent = 'Edit Cron Job';
            document.getElementById('job-id').value = job.id;
            document.getElementById('job-name').value = job.name;
            document.getElementById('job-schedule').value = job.schedule;
            document.getElementById('job-description').value = job.description || '';
            document.getElementById('job-enabled').checked = job.enabled;
            
            // Primary webhook
            document.getElementById('primary-url').value = job.primary.url;
            document.getElementById('primary-method').value = job.primary.method;
            document.getElementById('primary-headers').value = job.primary.headers ? JSON.stringify(job.primary.headers, null, 2) : '';
            document.getElementById('primary-body').value = job.primary.body || '';
            document.getElementById('primary-bearer-token').value = job.primary.bearer_token || '';
            document.getElementById('primary-timeout').value = job.primary.timeout || 0;

            // Secondary webhook
            if (job.secondary) {
                document.getElementById('secondary-enabled').checked = job.secondary.enabled !== false; // Default to true if not set
                document.getElementById('secondary-url').value = job.secondary.url || '';
                document.getElementById('secondary-method').value = job.secondary.method || 'GET';
                document.getElementById('secondary-headers').value = job.secondary.headers ? JSON.stringify(job.secondary.headers, null, 2) : '';
                document.getElementById('secondary-bearer-token').value = job.secondary.bearer_token || '';
                document.getElementById('secondary-jq-selectors').value = job.secondary.jq_selectors ? JSON.stringify(job.secondary.jq_selectors, null, 2) : '';
                document.getElementById('secondary-body-template').value = job.secondary.body_template || '';
                document.getElementById('secondary-timeout').value = job.secondary.timeout || 0;
            } else {
                // Reset secondary timeout if no secondary webhook
                document.getElementById('secondary-enabled').checked = false;
                document.getElementById('secondary-timeout').value = 0;
            }
            
            document.getElementById('save-output').checked = job.save_output || false;
            
            document.getElementById('job-modal').style.display = 'block';
        })
        .catch(error => {
            console.error('Error loading job:', error);
            alert('Failed to load job details');
        });
}

function closeModal() {
    document.getElementById('job-modal').style.display = 'none';
}

// Job management
function saveJob(event) {
    event.preventDefault();
    
    const jobId = document.getElementById('job-id').value;
    const isNew = !jobId;
    
    // Parse headers and jq selectors
    let primaryHeaders = {};
    let secondaryHeaders = {};
    let secondaryJQSelectors = {};

    try {
        const primaryHeadersText = document.getElementById('primary-headers').value;
        if (primaryHeadersText.trim()) {
            primaryHeaders = JSON.parse(primaryHeadersText);
        }

        const secondaryHeadersText = document.getElementById('secondary-headers').value;
        if (secondaryHeadersText.trim()) {
            secondaryHeaders = JSON.parse(secondaryHeadersText);
        }

        const secondaryJQText = document.getElementById('secondary-jq-selectors').value;
        if (secondaryJQText.trim()) {
            secondaryJQSelectors = JSON.parse(secondaryJQText);
        }
    } catch (error) {
        alert('Invalid JSON in headers or jq selectors field');
        return;
    }
    
    const job = {
        id: jobId || generateId(),
        name: document.getElementById('job-name').value,
        schedule: document.getElementById('job-schedule').value,
        description: document.getElementById('job-description').value,
        enabled: document.getElementById('job-enabled').checked,
        primary: {
            url: document.getElementById('primary-url').value,
            method: document.getElementById('primary-method').value,
            headers: primaryHeaders,
            body: document.getElementById('primary-body').value,
            bearer_token: document.getElementById('primary-bearer-token').value,
            timeout: parseInt(document.getElementById('primary-timeout').value) || 0
        },
        save_output: document.getElementById('save-output').checked
    };

    // Add secondary webhook if URL is provided
    const secondaryUrl = document.getElementById('secondary-url').value;
    if (secondaryUrl) {
        job.secondary = {
            enabled: document.getElementById('secondary-enabled').checked,
            url: secondaryUrl,
            method: document.getElementById('secondary-method').value,
            headers: secondaryHeaders,
            bearer_token: document.getElementById('secondary-bearer-token').value,
            jq_selectors: Object.keys(secondaryJQSelectors).length > 0 ? secondaryJQSelectors : undefined,
            body_template: document.getElementById('secondary-body-template').value || undefined,
            only_if_vars_non_empty: document.getElementById('only-if-vars-non-empty').checked,
            timeout: parseInt(document.getElementById('secondary-timeout').value) || 0
        };
    }
    
    const url = isNew ? '/api/jobs' : `/api/jobs/${jobId}`;
    const method = isNew ? 'POST' : 'PUT';
    
    fetch(url, {
        method: method,
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(job)
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
    })
    .then(() => {
        closeModal();
        location.reload(); // Simple refresh for now
    })
    .catch(error => {
        console.error('Error saving job:', error);
        alert('Failed to save job: ' + error.message);
    });
}

function deleteJob(jobId) {
    if (!confirm('Are you sure you want to delete this job?')) {
        return;
    }
    
    fetch(`/api/jobs/${jobId}`, {
        method: 'DELETE'
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        location.reload();
    })
    .catch(error => {
        console.error('Error deleting job:', error);
        alert('Failed to delete job');
    });
}

function testJob(jobId) {
    const button = event.target;
    const originalText = button.textContent;
    
    button.textContent = 'Testing...';
    button.disabled = true;
    
    fetch(`/api/jobs/test/${jobId}`, {
        method: 'POST'
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        
        button.textContent = 'Tested!';
        button.classList.add('btn-success');
        
        setTimeout(() => {
            button.textContent = originalText;
            button.classList.remove('btn-success');
            button.disabled = false;
        }, 2000);
    })
    .catch(error => {
        console.error('Error testing job:', error);
        alert('Failed to test job: ' + error.message);
        
        button.textContent = originalText;
        button.disabled = false;
    });
}

// Utility functions
function generateId() {
    return Date.now().toString(36) + Math.random().toString(36).substr(2);
}

// Close modal when clicking outside
window.onclick = function(event) {
    const modal = document.getElementById('job-modal');
    if (event.target === modal) {
        closeModal();
    }
}

// Initialize theme on page load
document.addEventListener('DOMContentLoaded', initTheme);

// Reminder management
function addReminder(jobId) {
    document.getElementById('reminder-job-id').value = jobId;
    document.getElementById('reminder-id').value = ''; // Clear for new reminder
    document.getElementById('reminder-form').reset();
    document.getElementById('reminder-modal').style.display = 'block';
    // Change title to "Add Reminder"
    document.querySelector('#reminder-modal .modal-header h2').textContent = 'Add Reminder';
}

function editReminder(reminderId, jobId, text, datetime) {
    document.getElementById('reminder-job-id').value = jobId;
    document.getElementById('reminder-id').value = reminderId;
    document.getElementById('reminder-text').value = text;

    // Convert UTC datetime to local datetime for display in input
    // The datetime comes in format "2006-01-02T15:04:05" (assumed to be UTC)
    const utcDate = new Date(datetime + 'Z'); // Add Z to indicate UTC

    // Helper function to pad numbers with leading zeros
    function pad(num) {
        return num < 10 ? '0' + num : num;
    }

    const localDatetime = utcDate.getFullYear() + '-' +
                         pad(utcDate.getMonth() + 1) + '-' +
                         pad(utcDate.getDate()) + 'T' +
                         pad(utcDate.getHours()) + ':' +
                         pad(utcDate.getMinutes());
    document.getElementById('reminder-datetime').value = localDatetime;

    document.getElementById('reminder-modal').style.display = 'block';
    // Change title to "Edit Reminder"
    document.querySelector('#reminder-modal .modal-header h2').textContent = 'Edit Reminder';
}

function closeReminderModal() {
    document.getElementById('reminder-modal').style.display = 'none';
}

function saveReminder(event) {
    event.preventDefault();

    const jobId = document.getElementById('reminder-job-id').value;
    const reminderId = document.getElementById('reminder-id').value;
    const text = document.getElementById('reminder-text').value;
    const datetime = document.getElementById('reminder-datetime').value;

    // Format datetime for Go parsing (RFC3339)
    const formattedDatetime = new Date(datetime).toISOString();

    const reminder = {
        id: reminderId || generateId(),
        text: text,
        datetime: formattedDatetime
    };

    if (reminderId) {
        // Update existing reminder
        updateExistingReminder(jobId, reminder);
    } else {
        // Add new reminder
        addNewReminder(jobId, reminder);
    }
}

function addNewReminder(jobId, reminder) {
    // Get current job data
    fetch(`/api/jobs/${jobId}`)
        .then(response => response.json())
        .then(job => {
            // Add reminder to job
            if (!job.reminders) {
                job.reminders = [];
            }
            job.reminders.push(reminder);

            // Update job
            return fetch(`/api/jobs/${jobId}`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(job)
            });
        })
        .then(response => {
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            return response.json();
        })
        .then(() => {
            closeReminderModal();
            location.reload(); // Simple refresh for now
        })
        .catch(error => {
            console.error('Error saving reminder:', error);
            alert('Failed to save reminder: ' + error.message);
        });
}

function updateExistingReminder(jobId, updatedReminder) {
    fetch(`/api/reminders/${jobId}/${updatedReminder.id}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(updatedReminder)
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
    })
    .then(() => {
        closeReminderModal();
        location.reload(); // Simple refresh for now
    })
    .catch(error => {
        console.error('Error updating reminder:', error);
        alert('Failed to update reminder: ' + error.message);
    });
}

function deleteReminder(reminderId, jobId) {
    if (!confirm('Are you sure you want to delete this reminder?')) {
        return;
    }

    fetch(`/api/reminders/${jobId}/${reminderId}`, {
        method: 'DELETE'
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        location.reload();
    })
    .catch(error => {
        console.error('Error deleting reminder:', error);
        alert('Failed to delete reminder');
    });
}

// Edit reminder functions
function editReminder(reminderId, jobId, text, datetime) {
    document.getElementById('edit-reminder-job-id').value = jobId;
    document.getElementById('edit-reminder-id').value = reminderId;
    document.getElementById('edit-reminder-text').value = text;

    // Convert UTC datetime to local datetime for display in input
    // The datetime comes in format "2006-01-02T15:04:05" (assumed to be UTC)
    const utcDate = new Date(datetime + 'Z'); // Add Z to indicate UTC

    // Helper function to pad numbers with leading zeros
    function pad(num) {
        return num < 10 ? '0' + num : num;
    }

    const localDatetime = utcDate.getFullYear() + '-' +
                         pad(utcDate.getMonth() + 1) + '-' +
                         pad(utcDate.getDate()) + 'T' +
                         pad(utcDate.getHours()) + ':' +
                         pad(utcDate.getMinutes());
    document.getElementById('edit-reminder-datetime').value = localDatetime;

    document.getElementById('edit-reminder-modal').style.display = 'block';
}

function closeEditReminderModal() {
    document.getElementById('edit-reminder-modal').style.display = 'none';
}

function updateReminder(event) {
    event.preventDefault();

    const jobId = document.getElementById('edit-reminder-job-id').value;
    const reminderId = document.getElementById('edit-reminder-id').value;
    const text = document.getElementById('edit-reminder-text').value;
    const datetime = document.getElementById('edit-reminder-datetime').value;

    // Format datetime for Go parsing (RFC3339)
    const formattedDatetime = new Date(datetime).toISOString();

    const updatedReminder = {
        id: reminderId,
        text: text,
        datetime: formattedDatetime
    };

    fetch(`/api/reminders/${jobId}/${reminderId}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(updatedReminder)
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
    })
    .then(() => {
        closeEditReminderModal();
        location.reload(); // Simple refresh for now
    })
    .catch(error => {
        console.error('Error updating reminder:', error);
        alert('Failed to update reminder: ' + error.message);
    });
}

// Close reminder modal when clicking outside
window.onclick = function(event) {
    const jobModal = document.getElementById('job-modal');
    const reminderModal = document.getElementById('reminder-modal');
    const editReminderModal = document.getElementById('edit-reminder-modal');

    if (event.target === jobModal) {
        closeModal();
    } else if (event.target === reminderModal) {
        closeReminderModal();
    } else if (event.target === editReminderModal) {
        closeEditReminderModal();
    }
}

// Convert UTC reminder times to local time on page load
document.addEventListener('DOMContentLoaded', function() {
    const reminderElements = document.querySelectorAll('.reminder-datetime');
    reminderElements.forEach(function(element) {
        const utcTime = element.getAttribute('data-utc');
        if (utcTime) {
            try {
                const utcDate = new Date(utcTime);
                // Helper function to pad numbers with leading zeros
                function pad(num) {
                    return num < 10 ? '0' + num : num;
                }
                // Format as YYYY-MM-DD HH:MM:SS in local time
                const localFormatted = utcDate.getFullYear() + '-' +
                                     pad(utcDate.getMonth() + 1) + '-' +
                                     pad(utcDate.getDate()) + ' ' +
                                     pad(utcDate.getHours()) + ':' +
                                     pad(utcDate.getMinutes()) + ':' +
                                     pad(utcDate.getSeconds());
                element.textContent = localFormatted;
            } catch (e) {
                console.error('Error converting time:', e);
            }
        }
    });
});