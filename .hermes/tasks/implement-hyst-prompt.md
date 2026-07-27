补上 _hyst_cycle 方法的定义。

## 在哪加
在 py-agent/ioc/hiaf_ioc_final.py 中，_pi_cycle 方法（744行）的**前面**插入 _hyst_cycle 方法。

## 实现
利用 hiaf_config 中已有的 HYST 常量：
- HYST_STEP_SMALL = 0.25（小步长）
- HYST_STEP_BIG = 0.50（大步长）
- VALVE_MIN / VALVE_MAX

逻辑：
1. 读当前阀位
2. error > HYST_TARGET_BAND 时，向减小误差方向步进（error > 0 压力低了→开大阀门）
3. error > 5 * HYST_TARGET_BAND 用大步长，否则用小步长
4. 钳位到 VALVE_MIN~VALVE_MAX
5. 通过 Piezo_ValveSP.write(new_valve) 写入

插入后运行：python3 -m py_compile py-agent/ioc/hiaf_ioc_final.py
