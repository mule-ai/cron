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

            // Secondary webhook
            if (job.secondary) {
                document.getElementById('secondary-url').value = job.secondary.url || '';
                document.getElementById('secondary-method').value = job.secondary.method || 'GET';
                document.getElementById('secondary-headers').value = job.secondary.headers ? JSON.stringify(job.secondary.headers, null, 2) : '';
                document.getElementById('secondary-bearer-token').value = job.secondary.bearer_token || '';
                document.getElementById('secondary-jq-selectors').value = job.secondary.jq_selectors ? JSON.stringify(job.secondary.jq_selectors, null, 2) : '';
                document.getElementById('secondary-body-template').value = job.secondary.body_template || '';
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
            bearer_token: document.getElementById('primary-bearer-token').value
        },
        save_output: document.getElementById('save-output').checked
    };
    
    // Add secondary webhook if URL is provided
    const secondaryUrl = document.getElementById('secondary-url').value;
    if (secondaryUrl) {
        job.secondary = {
            url: secondaryUrl,
            method: document.getElementById('secondary-method').value,
            headers: secondaryHeaders,
            bearer_token: document.getElementById('secondary-bearer-token').value,
            jq_selectors: Object.keys(secondaryJQSelectors).length > 0 ? secondaryJQSelectors : undefined,
            body_template: document.getElementById('secondary-body-template').value || undefined,
            only_if_vars_non_empty: document.getElementById('only-if-vars-non-empty').checked
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