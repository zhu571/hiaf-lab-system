ADD AI chat + NL execute features to the instrument control page.

## Context

Working directory: `/tmp/hiaf-lab-system`

The Go backend now has `POST /api/v1/instruments/{id}/nl-execute` that:
1. Translates NL input to command via py-agent
2. Executes the command via InstrumentWorker
3. Returns `{status, command, scpi, explanation, response, parsed_value, parsed_points, plot_type, duration_ms, error}`

The existing `POST /api/v1/instruments/{id}/nl-commands` (translate-only) is also available.

## What to read first

Read these files before making any changes:
1. `web-ui/src/views/InstrumentsView.vue` — existing instrument page (Piezo control + instrument cards + manual command execution)
2. `web-ui/src/api/instruments.ts` — existing API client (add nl-commands and nl-execute if missing)
3. `web-ui/src/api/client.ts` — Axios instance setup
4. `web-ui/package.json` — check existing dependencies

## Task 1: Add API functions

In `web-ui/src/api/instruments.ts`, add (only if missing):
```ts
// NL interpretation (translate only, no execution)
export function interpretNL(instrumentId: string, input: string, history: Array<{role: string, content: string}>) {
  return client.post(`/instruments/${instrumentId}/nl-commands`, { input, history }).then(r => r.data.data)
}

// NL execute (translate + execute + store result)
export function executeNL(instrumentId: string, input: string, history: Array<{role: string, content: string}>) {
  return client.post(`/instruments/${instrumentId}/nl-execute`, { input, history }).then(r => r.data.data)
}
```

Also add types:
```ts
export interface NLCommandCandidate {
  status: string           // "ok" | "clarify" | "rejected"
  command?: string
  risk?: string
  scpi_preview?: string
  explanation?: string
  question?: string
  reason?: string
}

export interface NLExecuteResult {
  status: string
  command: string
  scpi: string
  explanation: string
  response: string
  parsed_value?: number
  parsed_points?: Array<{x: number, y: number}>
  plot_type?: string
  duration_ms: number
  error: string
}
```

## Task 2: Add AI chat section to InstrumentsView.vue

Add a new section between the instrument cards and the whitelist table. The section should be visible on desktop (hidden on mobile or collapsed by default).

Components needed:
- Text input + send button for NL input
- History of chat messages (user + assistant)
- For "clarify"/"rejected" responses: show explanation/reason text
- For "ok" responses with command: show the interpreted command + "执行" button
- After execution: show result text + duration
- If parsed_points exist: render a line chart using vue-echarts

### Install echarts first:
```bash
cd web-ui && npm install echarts vue-echarts
```

### Template structure (add after the instrument cards section, before whitelist section):

```html
<!-- AI 自然语言控制 -->
<section class="panel" v-if="!isMobile">
  <div class="panel-head">
    <h3 class="panel-title">AI 对话控制</h3>
    <span class="muted hint">用自然语言控制仪器，例如"读取仪器标识"、"设置频率1M到8M"</span>
  </div>
  <div class="nl-chat">
    <div class="nl-messages" ref="nlMessagesRef">
      <div v-for="(msg, i) in nlHistory" :key="i" class="nl-msg" :class="msg.role">
        <div class="nl-msg-content">
          <p>{{ msg.content }}</p>
          <!-- Show interpreted command -->
          <div v-if="msg.command" class="nl-cmd-card">
            <code>{{ msg.scpi }}</code>
            <span class="muted">{{ msg.explanation }}</span>
          </div>
          <!-- Execute button for translated commands -->
          <el-button v-if="msg.command && !msg.executed && !msg.executing" type="primary" size="small" @click="executeNLCommand(i)">执行</el-button>
          <el-button v-if="msg.executing" type="primary" size="small" loading>执行中...</el-button>
          <!-- Execution result -->
          <div v-if="msg.result" class="nl-result">
            <pre v-if="msg.result.response" class="cmd-response">{{ msg.result.response }}</pre>
            <p class="muted">耗时 {{ msg.result.duration_ms }} ms</p>
            <!-- Chart for scan data -->
            <div v-if="msg.result.parsed_points && msg.result.parsed_points.length > 0" class="nl-chart">
              <v-chart :option="buildChartOption(msg.result.parsed_points)" autoresize style="height: 300px" />
            </div>
          </div>
          <p v-if="msg.error" class="stat-error">{{ msg.error }}</p>
        </div>
      </div>
      <el-empty v-if="!nlHistory.length" description="输入自然语言指令，AI 帮你翻译为仪器命令" :image-size="60" />
    </div>
    <div class="nl-input-row">
      <el-input v-model="nlInput" placeholder="如：帮我看一下S11数据" @keyup.enter="sendNL" :disabled="nlBusy" />
      <el-button type="primary" :loading="nlBusy" @click="sendNL">发送</el-button>
    </div>
  </div>
</section>
```

### Script additions:

```ts
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

// NL chat state
interface NLMessage {
  role: 'user' | 'assistant'
  content: string
  command?: string
  scpi?: string
  explanation?: string
  executed?: boolean
  executing?: boolean
  result?: NLExecuteResult
  error?: string
}

const nlInput = ref('')
const nlBusy = ref(false)
const nlHistory = ref<NLMessage[]>([])
const nlMessagesRef = ref<HTMLElement>()
const isMobile = ref(false) // reuse existing if available, or add

function scrollNLToBottom() {
  nextTick(() => {
    const el = nlMessagesRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

async function sendNL() {
  const input = nlInput.value.trim()
  if (!input || nlBusy.value) return
  nlInput.value = ''
  nlHistory.value.push({ role: 'user', content: input })
  nlBusy.value = true
  scrollNLToBottom()

  try {
    const candidate = await interpretNL(expandedId.value, input, [])
    const msg: NLMessage = { role: 'assistant', content: '' }
    
    if (candidate.status === 'ok' && candidate.command) {
      msg.content = `理解：${candidate.explanation || candidate.command}`
      msg.command = candidate.command
      msg.scpi = candidate.scpi_preview || ''
      msg.explanation = candidate.explanation || ''
    } else if (candidate.status === 'clarify') {
      msg.content = candidate.explanation || candidate.question || '需要更多信息'
    } else if (candidate.status === 'rejected') {
      msg.content = candidate.reason || '无法执行该指令'
    } else {
      msg.content = 'AI 翻译失败'
    }
    nlHistory.value.push(msg)
  } catch (err) {
    nlHistory.value.push({ role: 'assistant', content: 'AI 翻译失败', error: err instanceof Error ? err.message : '未知错误' })
  } finally {
    nlBusy.value = false
    scrollNLToBottom()
  }
}

async function executeNLCommand(index: number) {
  const msg = nlHistory.value[index]
  if (!msg.command || msg.executing) return
  msg.executing = true
  try {
    const result = await executeNL(expandedId.value, '', []) // use the same input from history
    msg.result = result
    msg.executed = true
    if (result.error) {
      msg.error = result.error
    }
  } catch (err) {
    msg.error = err instanceof Error ? err.message : '执行失败'
  } finally {
    msg.executing = false
  }
}

function buildChartOption(points: Array<{x: number, y: number}>) {
  return {
    grid: { top: 20, right: 20, bottom: 40, left: 50 },
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'value', name: 'Frequency (Hz)' },
    yAxis: { type: 'value', name: 'Value' },
    series: [{
      data: points.map(p => [p.x, p.y]),
      type: 'line',
      smooth: true,
      symbol: 'none',
      sampling: points.length > 2000 ? 'lttb' : undefined,
    }]
  }
}
```

Also add `import { nextTick } from 'vue'` to existing imports.

## Task 3: Verify

```bash
cd web-ui && npm install echarts vue-echarts && npm run build
```

Fix any TypeScript errors. The build must pass cleanly.
