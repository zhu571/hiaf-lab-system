翻译 web-ui/src/views/SensorsView.vue 的所有中文到英文。

把中文替换为 t('sensors.xxx') 或 $t('sensors.xxx')。
加 import { useI18n } from 'vue-i18n' 和 const { t } = useI18n()。

注意：MEASUREMENTS 和 RANGES 数组在 script 中，label 需要改成用 t() 的 computed。

完成后 npm run build 验证。
