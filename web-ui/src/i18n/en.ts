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
  assembly: {
    // Assembly steps page (AssemblyView).
    title: 'Assembly Steps',
    allStatus: 'All statuses',
    aiGenerate: 'AI Generate Steps',
    templateLibrary: 'Template Library',
    create: 'New Step',
    retry: 'Retry',
    dragSort: 'Drag to reorder',
    // Step meta line prefixes, each followed by a value.
    metaAssignee: 'Assignee: ',
    metaDependency: 'Depends on: ',
    metaStarted: 'Started: ',
    metaCompleted: 'Completed: ',
    delete: 'Delete',
    empty: 'No assembly steps',
    name: 'Name',
    description: 'Description',
    dependsOn: 'Depends on',
    noDependency: 'No dependency',
    assignee: 'Assignee',
    unassigned: 'Unassigned',
    save: 'Save',
    templateName: 'Template name (used when saving as template)',
    aiPromptPlaceholder:
      'Describe the assembly process in natural language, e.g.: clean the chamber and check the O-ring first, then install the target holder, finally evacuate and leak-check',
    generate: 'Generate',
    backToEdit: 'Back to edit',
    apply: 'Apply directly',
    saveTemplate: 'Save template',
    saveAndApply: 'Save & apply',
    // Dialog shown when starting/resuming a step whose dependency is not completed.
    overrideTitle: 'Prerequisite step not completed',
    overrideTip: 'Prerequisite step "{name}" is currently',
    overridePlaceholder: 'If the prerequisite step was cancelled, provide a reason to force override (override_reason)',
    overrideSubmit: 'Proceed anyway',
    loadFailed: 'Failed to load assembly steps',
    statusUpdated: 'Status updated',
    transitionFailed: 'Status transition failed',
    confirmTransition: 'Confirm {action} step "{name}"?',
    transitionTitle: 'Status Transition',
    orderUpdated: 'Order updated',
    reorderFailed: 'Reorder failed',
    nameRequired: 'Please enter the step name',
    created: 'Step created',
    createFailed: 'Failed to create step',
    confirmDelete: 'Delete step "{name}"?',
    deleteTitle: 'Delete Step',
    deleted: 'Step deleted',
    deleteFailed: 'Failed to delete step',
    clarifyNeeded: 'More info needed: {question}',
    clarifyMore: 'More information is needed. Please refine the description and try again.',
    generateFailedReason: 'Cannot generate: {reason}',
    generateFailed: 'Cannot generate steps. Please adjust the description and try again.',
    aiFailed: 'AI generation failed',
    itemsCount: 'Number of candidate steps must be between 1 and 30',
    allNamesRequired: 'Please fill in all step names',
    applied: 'Steps applied to the current project',
    applyFailed: 'Failed to apply',
    templateNameRequired: 'Please enter the template name',
    templateSaved: 'Template saved. View it in the template library.',
    templateSaveFailed: 'Failed to save template',
    templateSavedApplied: 'Template saved and applied to the current project',
    saveApplyFailed: 'Failed to save and apply',
    // Assembly step status filter options.
    status: {
      planned: 'Planned',
      in_progress: 'In progress',
      paused: 'Paused',
      completed: 'Completed',
      skipped: 'Skipped',
      cancelled: 'Cancelled'
    },
    // Step transition action buttons (kept in sync with the backend state machine).
    action: {
      start: 'Start',
      cancel: 'Cancel',
      pause: 'Pause',
      complete: 'Complete',
      skip: 'Skip',
      resume: 'Resume',
      restart: 'Restart'
    }
  },
  gasControl: {
    // GasCell pressure control page (GasControlView).
    title: 'Gas Control',
    subtitle: 'Read-only realtime dashboard. The control loop and safety interlocks run on the IOC.',
    realtime: 'Live',
    reconnecting: 'Reconnecting',
    a5Trip: 'A5 interlock tripped (code {code})',
    dataInvalid: 'Invalid data',
    chartTitle: 'A1 / Valve / Setpoint',
    chartHint: 'Last 120 valid samples',
    loadFailed: 'Failed to load data',
    panel: 'Control Panel',
    panelHint: 'Every write is validated by the backend and confirmed by readback.',
    applyParams: 'Apply',
    start: 'Start',
    stop: 'Stop',
    manualValve: 'Manual valve %',
    setValve: 'Set Valve',
    setA5Max: 'Set A5Max',
    clearA5: 'Clear A5 Interlock',
    // Status card labels.
    a1Pressure: 'A1 Pressure',
    setpoint: 'Setpoint',
    valveOpening: 'Valve Opening',
    controlError: 'Control Error',
    runningState: 'Running State',
    running: 'Running',
    stopped: 'Stopped',
    snapshotFailed: 'Failed to load snapshot',
    streamInterrupted: 'Realtime connection lost. The browser is reconnecting automatically.',
    chartValve: 'Valve (%)',
    writeFailed: 'Write failed',
    paramRequired: 'Please fill in at least one parameter',
    paramsWritten: 'Parameters written and confirmed by readback',
    startSuccess: 'Control started',
    stopSuccess: 'Control stopped',
    valveRequired: 'Please enter the manual valve position',
    valveWritten: 'Valve position written and confirmed by readback',
    a5MaxRequired: 'Please enter A5Max',
    a5MaxConfirm: 'Changing the safety threshold affects the A5 overpressure interlock. Continue?',
    a5MaxConfirmTitle: 'Safety Threshold Confirmation',
    a5MaxWritten: 'A5Max written and confirmed by readback',
    clearA5Confirm: 'Confirm on-site conditions are safe. Clear the alarm and unlock?',
    a5Cleared: 'A5 interlock cleared'
  },
  sensors: {
    // Sensor data page (SensorsView).
    title: 'Sensor Data',
    autoRefresh: 'Auto refresh',
    refresh: 'Refresh',
    latest: 'Latest Readings',
    allMeasurements: 'All measurements',
    retry: 'Retry',
    noReadings: 'No readings',
    history: 'History',
    historyHint: 'each series normalized independently',
    noDataInRange: 'No data in the selected range',
    latestFailed: 'Failed to load latest readings',
    historyFailed: 'Failed to load history',
    // InfluxDB measurement selector options.
    measurement: {
      pressure: 'Pressure',
      vacuum: 'Vacuum',
      control: 'Control',
      temperature: 'Temperature',
      pump: 'Pump'
    },
    // History time-range options.
    range: {
      '1h': 'Last 1 hour',
      '6h': 'Last 6 hours',
      '24h': 'Last 24 hours',
      '7d': 'Last 7 days'
    }
  },
  testData: {
    // Test data page (TestDataView).
    title: 'Test Data',
    entry: 'Entry',
    entryTitle: 'Enter Test Data',
    dataType: 'Data Type',
    dataTypePlaceholder: 'Select data type',
    measurement: 'Measurement',
    measurementPlaceholder: 'e.g. beam_current',
    value: 'Value',
    unit: 'Unit',
    unitPlaceholder: 'e.g. K / mbar / V',
    quality: 'Quality',
    measuredAt: 'Measured At',
    timePlaceholder: 'Select time (optional)',
    linkedRun: 'Linked Run',
    runPlaceholder: 'Select run (optional)',
    notes: 'Notes',
    notesPlaceholder: 'Notes (optional)',
    submit: 'Submit',
    list: 'Data List',
    allTypes: 'All types',
    allQualities: 'All qualities',
    retry: 'Retry',
    source: 'Source',
    actions: 'Actions',
    markInvalid: 'Mark Invalid',
    empty: 'No data',
    chart: 'Trend',
    chartHint: 'grouped by measurement, each series normalized independently',
    loadFailed: 'Failed to load test data',
    runsLoadFailed: 'Failed to load runs',
    dataTypeRequired: 'Please select a data type',
    measurementRequired: 'Please enter the measurement',
    valueRequired: 'Please enter a value',
    created: 'Test data recorded',
    createFailed: 'Failed to record',
    invalidateConfirm: 'Mark this record as invalid?',
    confirm: 'OK',
    invalidated: 'Marked as invalid',
    invalidateFailed: 'Failed to mark invalid'
  },
  runList: {
    // Experiment run list page (RunListView).
    title: 'Experiment Runs',
    status: 'Status',
    all: 'All',
    searchCampaign: 'Search campaign',
    create: 'New Run',
    retry: 'Retry',
    empty: 'No experiment runs',
    noCampaign: 'No campaign set',
    createdAt: 'Created: ',
    nameLabel: 'Name (required)',
    type: 'Type',
    gasType: 'Gas',
    targetTemp: 'Target Temp',
    minTemp: 'Min Temp',
    optional: 'Optional',
    pressureMin: 'Pressure Min',
    pressureMax: 'Pressure Max',
    pressureUnit: 'Pressure Unit',
    hasBeam: 'With Beam',
    devices: 'Devices',
    devicesPlaceholder: 'Select devices',
    description: 'Description',
    save: 'Save',
    loadFailed: 'Failed to load runs',
    nameRequired: 'Please enter the run name',
    created: 'Run created',
    createFailed: 'Failed to create run',
    // Run status filter options.
    runStatus: {
      planned: 'Planned',
      active: 'Active',
      paused: 'Paused',
      completed: 'Completed',
      aborted: 'Aborted'
    },
    // Run type options.
    runType: {
      cooldown: 'Cooldown',
      warmup: 'Warmup',
      steady_state: 'Steady State',
      test: 'Test'
    }
  },
  issues: {
    // Issue board page (IssuesView).
    title: 'Issue Board',
    create: 'New Issue',
    empty: 'No issues',
    detail: 'Issue Detail',
    reasonPlaceholder: 'Reason for status change',
    updateStatus: 'Update Status',
    fieldTitle: 'Title',
    severity: 'Severity',
    description: 'Description',
    save: 'Save',
    statusUpdated: 'Status updated',
    statusUpdateFailed: 'Failed to update status',
    // Board column / status labels.
    status: {
      open: 'Open',
      in_progress: 'In Progress',
      resolved: 'Resolved',
      closed: 'Closed'
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
