// English messages. Context notes are kept as comments next to each group
// (equivalent to the `_comment` keys planned for a JSON format — using .ts
// modules, native comments avoid polluting the runtime message map).
export default {
  common: {
    cancel: 'Cancel',
    // Device connection state badges on the dashboard.
    online: 'Online',
    offline: 'Offline'
  },
  login: {
    // Tagline under the "HIAF Lab System" brand on the login page.
    subtitle: 'Lab Log Management Platform',
    username: 'Username',
    password: 'Password',
    submit: 'Log in',
    noAccount: 'No account yet?',
    register: 'Sign up',
    registerTitle: 'Create account',
    usernamePlaceholder: '2-32 characters',
    passwordPlaceholder: 'At least 8 characters',
    confirmPassword: 'Confirm password',
    loginFailed: 'Login failed',
    registerFailed: 'Registration failed',
    usernameLength: 'Username must be 2-32 characters',
    passwordLength: 'Password must be at least 8 characters',
    passwordMismatch: 'Passwords do not match'
  },
  nav: {
    // Left sidebar navigation (AppLayout).
    home: 'Home',
    projects: 'Projects',
    dailyReport: 'Daily Reports',
    experiences: 'Experiences',
    attachments: 'Attachments',
    aiReview: 'AI Review',
    // Section header above the system-level menu entries.
    systemGroup: 'System',
    gasControl: 'Gas Control',
    instruments: 'Instruments',
    sensors: 'Sensors',
    adminUsers: 'User Management',
    audit: 'Audit',
    settings: 'Settings',
    logout: 'Log out',
    // Short labels for the mobile bottom navigation bar.
    short: {
      dailyReport: 'Reports',
      experiences: 'Library',
      attachments: 'Files',
      mine: 'Me'
    }
  },
  dashboard: {
    title: 'Lab Dashboard',
    subtitle: 'Device status and team activity at a glance',
    deviceStatus: 'Device Status',
    // Panel badge: "{online}/{total} online" (instruments + gas control).
    onlineCount: '{online}/{total} online',
    noDevices: 'No devices',
    gasControl: 'Gas Control',
    runningState: 'Running State',
    a1Pressure: 'A1 Pressure',
    running: 'Running',
    stopped: 'Stopped',
    // Middle column: merged summary of the team's daily reports.
    brief: 'Daily Brief',
    last7Days: 'Last 7 days',
    // Brief card badge: number of people who filed a report that day.
    peopleCount: '{n} people',
    noReport: 'No report',
    teamReports: 'Team Daily Reports',
    reportsCount: '{n} reports',
    noReportToday: 'No reports for this day',
    noSummary: 'No summary',
    today: 'Today',
    yesterday: 'Yesterday',
    loadDevicesFailed: 'Failed to load devices',
    loadGasFailed: 'Failed to load gas cell status',
    loadReportsFailed: 'Failed to load daily reports'
  },
  project: {
    // Project workspace header (ProjectLayout).
    backToList: 'Projects',
    switchProject: 'Switch project',
    goToProjects: 'Go to projects',
    fallbackNoAccess: 'Project not found or access denied. Please select another one.',
    fallbackNoProjects: 'No projects yet. Create or select a project first.',
    tabs: {
      overview: 'Overview',
      issues: 'Issues',
      runs: 'Runs',
      testData: 'Test Data',
      rfMatching: 'RF Matching',
      assembly: 'Assembly'
    },
    // Project lifecycle stage tag in the workspace header.
    stages: {
      draft: 'Draft',
      active: 'Active',
      completed: 'Completed',
      archived: 'Archived',
      unknown: 'Unknown'
    }
  },
  settings: {
    title: 'Settings',
    mustChangePassword: 'Password change required on first login',
    language: 'Language / 语言',
    oldPassword: 'Current password',
    newPassword: 'New password',
    confirmNewPassword: 'Confirm new password',
    changePassword: 'Change password',
    quickLinks: 'Quick Links',
    passwordMismatch: 'Passwords do not match',
    passwordChanged: 'Password updated',
    languageSaved: 'Language preference saved',
    languageSaveFailed: 'Failed to save language preference'
  }
}
