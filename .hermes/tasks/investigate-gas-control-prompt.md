检查气压自动控制程序和手动调阀失效的原因。

用户报告：GasCell 自动控制（启动/停止）和手动调阀都失效了。需要排查是前端还是后端的 bug。

## 相关文件
- /home/zhuhaofan/hiaf-lab-system/web-ui/src/views/GasControlView.vue
- /home/zhuhaofan/hiaf-lab-system/web-ui/src/api/instruments.ts
- /home/zhuhaofan/hiaf-lab-system/go-server/instruments/handler.go
- /home/zhuhaofan/hiaf-lab-system/go-server/instruments/service.go
- /home/zhuhaofan/hiaf-lab-system/go-server/main.go

## 排查方向
1. 前端 API 调用路径是否正确（gasCellStart/gasCellStop/gasCellValve 是否指向正确的后端路由）
2. 后端路由是否注册
3. 后端 handler 是否存在
4. 后端 service 层是否正确执行（WriteGasCellPV 是否正常工作）
5. 是否有异常日志或错误返回
6. gasCellStatus() 的调用方式可能有问题（`(await gasCellStatus()).data` 双重解包）

## 输出
把排查结果写到 .hermes/investigations/gas-control-failure.md
