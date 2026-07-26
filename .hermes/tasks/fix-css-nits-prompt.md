根据审查结果修复 DashboardView.vue 的样式问题。

审查发现：
1. 设备卡片的 `.device-name` 缺少 `min-width: 0`，长仪器名会溢出而不是截断。在 `.device-name` 样式加一行 `min-width: 0;`
2. `.brief-card` 的 `width` 与 `flex-basis` 重复（两个都设了 210px 和 186px），删掉重复的 `width` 属性

只改 CSS 部分，不改功能和模板。

文件：/home/zhuhaofan/hiaf-lab-system/web-ui/src/views/DashboardView.vue
验证：npm run build
