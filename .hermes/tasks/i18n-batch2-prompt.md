翻译以下页面的中文文字为英文，沿用已有的 i18n 模式。

需要改的页面：
1. views/GasControlView.vue
2. views/SensorsView.vue
3. views/InstrumentMeasureView.vue

规则：
- 模板中用 $t('key')，script 中用 t('key')
- key 用页面名做前缀（gasControl.xxx, sensors.xxx, instrument.xxx）
- 已翻译过的 key 不重复加
- element-plus 的 label/placeholder 用 :label="$t('key')" / :placeholder="$t('key')"
- 不要改功能、样式、逻辑
- 完成后 npm run build 验证

让 AI 自行决定每个中文对应什么 key，覆盖所有中文文字。
