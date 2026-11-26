import { describe, it, expect, vi, beforeEach } from 'vitest'
import { startAssistantRunStream, type AssistantRunResult } from '../assistant'

function sseResponse(chunks: string[]) {
  const encoder = new TextEncoder()
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    }
  })
  return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}

const RESULT: AssistantRunResult = {
  run: {
    id: 'r1',
    session_id: 's1',
    user_id: 'u1',
    status: 'done',
    model: 'py-agent',
    prompt_version: 'assistant-chat-v1',
    input_tokens: 0,
    output_tokens: 0,
    started_at: '2026-11-26T10:00:00Z'
  },
  messages: [
    { id: 'm1', session_id: 's1', role: 'user', content: 'hi', created_at: '2026-11-26T10:00:00Z' },
    { id: 'm2', session_id: 's1', role: 'assistant', content: 'hello', created_at: '2026-11-26T10:00:01Z' }
  ]
}

describe('startAssistantRunStream', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  it('parses start/result/done events', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse([
        'event: start\ndata: {"status":"running"}\n\n',
        `event: result\ndata: ${JSON.stringify(RESULT)}\n\n`,
        'event: done\ndata: {}\n\n'
      ])
    )
    vi.stubGlobal('fetch', fetchMock)
    const onStart = vi.fn()
    const onResult = vi.fn()
    await startAssistantRunStream('s1', 'hi', { onStart, onResult })
    expect(onStart).toHaveBeenCalledOnce()
    expect(onResult).toHaveBeenCalledWith(RESULT)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/assistant/sessions/s1/runs/stream',
      expect.objectContaining({ method: 'POST', credentials: 'include' })
    )
  })

  it('reports error events', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        sseResponse(['event: error\ndata: {"code":"quota_exceeded","message":"今日额度已用完"}\n\n'])
      )
    )
    const onError = vi.fn()
    const onResult = vi.fn()
    await startAssistantRunStream('s1', 'hi', { onResult, onError })
    expect(onError).toHaveBeenCalledWith('今日额度已用完')
    expect(onResult).not.toHaveBeenCalled()
  })

  it('throws on non-2xx response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { message: 'forbidden' } }), {
          status: 403,
          headers: { 'Content-Type': 'application/json' }
        })
      )
    )
    await expect(startAssistantRunStream('s1', 'hi', { onResult: vi.fn() })).rejects.toThrow('forbidden')
  })
})
