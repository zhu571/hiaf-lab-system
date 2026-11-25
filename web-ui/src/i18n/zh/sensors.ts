export default {
  sensors: {
    title: '传感器数据',
    subtitle: 'InfluxDB 实时读数与历史趋势',
    autoRefresh: '自动刷新',
    refresh: '刷新',
    latest: '最新读数',
    selectMeasurements: '选择测量项',
    measurementLabel: '测量项',
    countInfo: '共 {n} 项',
    lastUpdated: '最后更新 {time}',
    stale: '过期',
    unknown: '未知测量项',
    retry: '重试',
    noReadings: '暂无读数',
    history: '历史趋势',
    historyHint: '每 30 秒自动刷新',
    noDataInRange: '所选时间范围内暂无数据',
    latestFailed: '最新读数加载失败',
    historyFailed: '历史数据加载失败',
    measurement: {
      pressure: '压力 pressure',
      vacuum: '真空 vacuum',
      control: '控制 control',
      temperature: '温度 temperature',
      pump: '泵 pump'
    },
    range: {
      '1h': '最近 1 小时',
      '6h': '最近 6 小时',
      '24h': '最近 24 小时',
      '7d': '最近 7 天'
    },
    chart: {
      zoomHint: 'Ctrl+滚轮缩放 · 拖拽平移 · 双击复位 · Shift+点击图例只看该条',
      customRange: '自定义',
      yAuto: '自动',
      yMin: '下限',
      yMax: '上限',
      reset: '复位',
      showAll: '全部显示'
    }
  },
}
