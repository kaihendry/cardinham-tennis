# Service Account Setup (Alternative to OAuth)

Service accounts are perfect for server-to-server authentication and don't require OAuth verification.

## Setup Steps

### 1. Create a Service Account

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Navigate to "APIs & Services" > "Credentials"
3. Click "Create Credentials" > "Service Account"
4. Give it a name like "Cardinham Calendar Service"
5. Click "Create and Continue"
6. Skip the optional steps and click "Done"

### 2. Create and Download Key

1. Click on your new service account
2. Go to the "Keys" tab
3. Click "Add Key" > "Create new key"
4. Choose "JSON" format
5. Download the key file and rename it to `service-account.json`

### 3. Share Calendar with Service Account

1. Get the service account email (looks like: `cardinham-calendar@project-id.iam.gserviceaccount.com`)
2. Go to your Google Calendar
3. Click the three dots next to your calendar name
4. Select "Settings and sharing"
5. Under "Share with specific people", click "Add people"
6. Add the service account email with "Make changes to events" permission

### 4. Update the Tool

The service account version will be much simpler - no OAuth flow needed! 