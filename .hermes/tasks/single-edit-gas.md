直接修改 web-ui/src/views/GasControlView.vue：

1. 在 import 区域加一行：
import { useI18n } from 'vue-i18n'

2. 在 script setup 开头加一行：
const { t } = useI18n()

3. 把 <h1>气压控制</h1> 改为 <h1>{{ $t('gasControl.title') }}</h1>

其他别改。验证 npm run build。
