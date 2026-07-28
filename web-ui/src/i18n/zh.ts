export default {
  common: {
    cancel: '取消',
    online: '在线',
    offline: '离线'
  },
  login: {
    subtitle: '实验室日志管理平台',
    username: '用户名',
    password: '密码',
    submit: '登录',
    noAccount: '还没有账户？',
    register: '注册',
    registerTitle: '注册账户',
    usernamePlaceholder: '2-32 个字符',
    passwordPlaceholder: '至少 8 位',
    confirmPassword: '确认密码',
    loginFailed: '登录失败',
    registerFailed: '注册失败',
    usernameLength: '用户名长度需为 2-32 个字符',
    passwordLength: '密码至少 8 位',
    passwordMismatch: '两次输入的密码不一致'
  },
  nav: {
    home: '首页',
    projects: '项目',
    dailyReport: '日报',
    experiences: '经验库',
    attachments: '附件',
    aiReview: 'AI审核',
    systemGroup: '系统',
    gasControl: '气压控制',
    instruments: '测量仪器',
    sensors: '传感器',
    adminUsers: '用户管理',
    audit: '审计',
    settings: '个人设置',
    logout: '退出登录',
    // 移动端底栏短标签
    short: {
      dailyReport: '日报',
      experiences: '经验',
      attachments: '附件',
      mine: '我的'
    }
  },
  dashboard: {
    title: '实验室仪表盘',
    subtitle: '设备运行状态与团队动态一览',
    deviceStatus: '设备状态',
    onlineCount: '{online}/{total} 在线',
    noDevices: '暂无设备',
    gasControl: '气压控制',
    runningState: '运行状态',
    a1Pressure: 'A1 压力',
    running: '运行中',
    stopped: '已停止',
    brief: '综合简报',
    last7Days: '近 7 天',
    peopleCount: '{n} 人',
    noReport: '暂无日报',
    teamReports: '团队成员日报',
    reportsCount: '{n} 篇',
    noReportToday: '当天暂无日报',
    noSummary: '暂无摘要',
    today: '今天',
    yesterday: '昨天',
    loadDevicesFailed: '设备列表加载失败',
    loadGasFailed: '气压状态加载失败',
    loadReportsFailed: '日报加载失败'
  },
  project: {
    backToList: '项目列表',
    switchProject: '切换项目',
    goToProjects: '前往项目列表',
    fallbackNoAccess: '项目不存在或无权访问，请重新选择',
    fallbackNoProjects: '暂无项目，请先创建或选择一个项目',
    tabs: {
      overview: '概览',
      issues: '问题',
      runs: '批次',
      testData: '数据',
      rfMatching: 'RF匹配',
      assembly: '装配'
    },
    stages: {
      draft: '筹备',
      active: '进行中',
      completed: '已完成',
      archived: '归档',
      unknown: '未知'
    }
  },
  settings: {
    title: '个人设置',
    mustChangePassword: '首次登录需要修改密码',
    language: '语言 / Language',
    oldPassword: '旧密码',
    newPassword: '新密码',
    confirmNewPassword: '确认新密码',
    changePassword: '修改密码',
    quickLinks: '快捷入口',
    passwordMismatch: '两次密码不一致',
    passwordChanged: '密码已修改',
    languageSaved: '语言偏好已保存',
    languageSaveFailed: '语言偏好保存失败'
  }
}
