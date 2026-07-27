修复 SensorsView.vue 中 `l.points is null` 的运行时错误。

文件：/home/zhuhaofan/hiaf-lab-system/web-ui/src/views/SensorsView.vue

## 问题
line 202: `latestPoints.value = [...data.points].sort(...)` — 当 `data.points` 为 null 时，展开运算符 `[...null]` 抛出 "can't access property Symbol.iterator"。
line 219: `historyPoints.value = data.points` — 可能设为 null，之后 line 69 的 `group.points.length` 报错。

## 修复
1. line 202: 改成 `latestPoints.value = data.points ? [...data.points].sort(...) : []`
2. line 219: 改成 `historyPoints.value = data.points || []`
3. 检查其他地方（如 line 69, 84, 153, 156, 161, 162, 165）是否也有 null points 的防御。这些地方已假设 points 是数组，但可能因 data.points 为 null 而收到 null 值。

验证：npm run build
