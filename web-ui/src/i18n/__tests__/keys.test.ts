import { describe, it, expect } from 'vitest'
import zh from '../zh'
import en from '../en'

// i18n key 一致性测试，覆盖三类事故：
// 1. 改 .vue 里 t('xxx.yyy') 忘记补 i18n key → 运行时空字符串（对比 src 内的字面量 key）
// 2. 只改了一种语言（如只加 en 忘加 zh）→ 另一种语言缺失文案（zh/en 双向对比）
// 3. en/zh 一方存在对方没有的多余 key
//
// zh/en 双向对比：对 export default 的消息树做全路径递归（含顶层 section key 与
// 任意深度的嵌套 key，如 nav.short.home），断言双向无缺失、无多余。
// .vue/.ts 字面量扫描：用 import.meta.glob（vite/client 类型，无需 @types/node）
// 读取 src 下所有 .vue/.ts 原文（?raw），只识别单引号完整调用 t('key') / $t('key')
// （key 以字母开头，排除 router 的 import('../views/...')、模板字符串 t(`a.${x}`) 等动态 key）；
// 动态拼装 key 无法静态判定，属已知盲区。

type MessageTree = Record<string, unknown>

function isPlainObject(value: unknown): value is MessageTree {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

// 收集消息树中所有叶子 key 的完整路径（'a.b.c'），空对象视为叶子（保留自身路径）
function collectPaths(tree: MessageTree, prefix = ''): string[] {
  const paths: string[] = []
  const keys = Object.keys(tree)
  if (keys.length === 0) {
    if (prefix) paths.push(prefix)
    return paths
  }
  for (const key of keys) {
    const path = prefix ? `${prefix}.${key}` : key
    const value = tree[key]
    if (isPlainObject(value)) {
      const nested = collectPaths(value, path)
      // 嵌套对象本身不是叶子，只收集其叶子路径
      paths.push(...nested)
    } else {
      paths.push(path)
    }
  }
  return paths
}

// 在消息树中解析 'a.b.c' 路径；任一段不存在返回 undefined
function resolvePath(tree: MessageTree, key: string): unknown {
  return key.split('.').reduce<unknown>((node, segment) => {
    if (node && typeof node === 'object' && segment in node) {
      return (node as MessageTree)[segment]
    }
    return undefined
  }, tree)
}

// src 下所有 .vue/.ts 源码原文（相对本文件 src/i18n/__tests__/ 的 glob；跳过 __tests__ 与 i18n 目录）
const SOURCE_FILES = import.meta.glob('../../**/*.{vue,ts}', {
  query: '?raw',
  import: 'default',
  eager: true
}) as Record<string, string>

// 扫描 src 下 .vue/.ts 中单引号字面量的 t('...') / $t('...') 调用
function collectTranslationUsages(): string[] {
  const keys = new Set<string>()
  const re = /\b(?:t|\$t)\('([A-Za-z][^']*)'\)/g
  for (const [file, content] of Object.entries(SOURCE_FILES)) {
    if (file.includes('__tests__') || file.includes('/i18n/')) continue
    let match: RegExpExecArray | null
    while ((match = re.exec(content)) !== null) keys.add(match[1])
  }
  return [...keys]
}

// 有意只保留单语的 key 豁免名单（当前为空）。
// 若未来确实需要（如某文案只有中文语境），在添加处注明理由，并保持 en/zh 双向豁免。
const EXEMPT_PATHS = new Set<string>([])

const zhPaths = new Set(collectPaths(zh).filter((p) => !EXEMPT_PATHS.has(p)))
const enPaths = new Set(collectPaths(en).filter((p) => !EXEMPT_PATHS.has(p)))

describe('i18n key 一致性', () => {
  it('zh/en 均存在 key（防空消息树导致测试空转）', () => {
    expect(zhPaths.size).toBeGreaterThan(0)
    expect(enPaths.size).toBeGreaterThan(0)
  })

  it('en 不缺失 zh 中已有的 key', () => {
    const missingInEn = [...zhPaths].filter((p) => !enPaths.has(p))
    expect(missingInEn, `en.ts 缺失 ${missingInEn.length} 个 key（需要补齐）:\n${missingInEn.join('\n')}`).toEqual([])
  })

  it('zh 不缺失 en 中已有的 key', () => {
    const missingInZh = [...enPaths].filter((p) => !zhPaths.has(p))
    expect(missingInZh, `zh.ts 缺失 ${missingInZh.length} 个 key（需要补齐）:\n${missingInZh.join('\n')}`).toEqual([])
  })

  it('无多余 key（en/zh 不应存在对方没有的 key）', () => {
    const extraInEn = [...enPaths].filter((p) => !zhPaths.has(p))
    const extraInZh = [...zhPaths].filter((p) => !enPaths.has(p))
    expect([...extraInEn, ...extraInZh], 'en/zh 存在单向多余 key（需要删除或双向补齐）').toEqual([])
  })

  it('src 内 .vue/.ts 用到的 t()/ $t() 字面量 key 均已在 zh.ts 定义（防改 .vue 忘加 key）', () => {
    const usedKeys = collectTranslationUsages()
    expect(usedKeys.length).toBeGreaterThan(0)
    const missing = usedKeys.filter((k) => resolvePath(zh, k) === undefined)
    expect(missing, `以下 key 在 .vue/.ts 中使用但 zh.ts 未定义（需要补齐 zh.ts 与 en.ts，或用动态 key）:\n${missing.join('\n')}`).toEqual([])
  })
})
