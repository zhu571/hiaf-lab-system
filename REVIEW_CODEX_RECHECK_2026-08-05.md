Reading additional input from stdin...
2026-08-05T10:47:32.074530Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:32.074665Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:32.825463Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:32.825530Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:33.106643Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:33.106760Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:33.374753Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:33.374919Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:33.626641Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:33.626732Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:33.884453Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:33.884542Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:34.005205Z ERROR codex_models_manager::manager: failed to refresh available models: stream disconnected before completion: failed to decode models response: missing field `models` at line 1 column 156; body: {"object":"list","data":[{"id":"deepseek-v4-flash","object":"model","owned_by":"deepseek"},{"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek"}]}
2026-08-05T10:47:34.276187Z ERROR codex_models_manager::manager: failed to refresh available models: stream disconnected before completion: failed to decode models response: missing field `models` at line 1 column 156; body: {"object":"list","data":[{"id":"deepseek-v4-flash","object":"model","owned_by":"deepseek"},{"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek"}]}
2026-08-05T10:47:34.534442Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:34.534516Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
OpenAI Codex v0.144.6
--------
workdir: /home/zhuhaofan/hiaf-lab-system
model: deepseek-v4-flash
provider: deepseek
approval: never
sandbox: danger-full-access
reasoning effort: medium
reasoning summaries: none
session id: 019fd189-2685-77f1-8d0d-81a2a6f34d41
--------
user
复审 HIAF 实验室系统本次未提交改动（终审后的低风险修复轮）。你是终审复核，上一轮你已审过并给出 4 条 🟡 建议，其中 2 条已按建议修复，请确认修复正确且未引入新问题，同时快速复查整体改动有无新问题。

## 本轮修复的 2 处（上一轮你的 🟡1、🟡3）
1. go-server/steptemplates/service.go:140 — 解码失败路径包 ErrUpstream：
   `return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)`
   （上一轮该处是裸 error 落 500，与 127/133/144 行归类不一致）
2. go-server/instruments/model.go:109 — ResultParserConfig.Type 注释补 sweep_xy_complex：
   `// "sweep_xy" | "sweep_xy_complex" | "single_value"`

## 审查对象（与上轮相同的全部改动，重点看这 2 处）
1. go-server/instruments/service.go — sweep_xy_complex parser + single_value 防御
2. go-server/instruments/service_test.go
3. go-server/instruments/whitelist_embedded.yaml
4. go-server/instruments/model.go（本轮改）
5. go-server/steptemplates/service.go（本轮改）
6. go-server/steptemplates/handler.go
7. go-server/steptemplates/service_test.go
8. py-agent/serve.py
9. web-ui/src/views/InstrumentMeasureView.vue
10. web-ui/src/api/instruments.ts
11. web-ui/src/i18n/zh.ts、en.ts

## 核查要点
1. 解码失败包 ErrUpstream 后：错误链是否完整？消息可读性？会不会影响现有测试（TestGenerateUpstreamErrorMarked 是否仍覆盖/需要新测试）？
2. 注释改动是否准确
3. 全局快速复查：有没有上轮遗漏的新问题
4. 本轮改动是否引入了测试失败（跑 go test 确认）

## 输出格式
- 🔴 硬伤（必须修）
- 🟡 建议改
- 🟢 好设计
- 最后一行给结论：可否上线（YES/NO）
只审核不改代码。
warning: Model metadata for `deepseek-v4-flash` not found. Defaulting to fallback metadata; this can degrade performance and cause issues.
2026-08-05T10:47:34.852375Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:34.852444Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:35.109501Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:35.109587Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:35.392384Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:35.392461Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:35.645561Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:35.645666Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:35.900741Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:35.900803Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
hook: SessionStart
hook: SessionStart Completed
hook: UserPromptSubmit
hook: UserPromptSubmit Completed
2026-08-05T10:47:38.195288Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:38.195359Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:43.469370Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:43.469419Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:43.724670Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:43.724738Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:43.976127Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:43.976234Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:45.382039Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:45.382101Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
codex
我来审查本轮未提交改动。先看 git diff 和关键文件。
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && git status && git diff --stat' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
On branch main
Your branch is up to date with 'origin/main'.

Changes not staged for commit:
  (use "git add/rm <file>..." to update what will be committed)
  (use "git restore <file>..." to discard changes in working directory)
	modified:   "docs/\344\273\252\345\231\250\347\231\275\345\220\215\345\215\225.yaml"
	modified:   go-server/instruments/model.go
	modified:   go-server/instruments/service.go
	modified:   go-server/instruments/service_test.go
	modified:   go-server/instruments/whitelist_embedded.yaml
	deleted:    go-server/static/assets/AdminUsersView-4m67EOip.js
	deleted:    go-server/static/assets/AdminUsersView-DgHiBl3v.css
	deleted:    go-server/static/assets/AgentCandidatesView-9LRfe0IO.js
	deleted:    go-server/static/assets/AgentCandidatesView-BexWTry6.css
	deleted:    go-server/static/assets/AssemblyView-Bt4Zawsu.js
	deleted:    go-server/static/assets/AssemblyView-l0xPRviP.css
	deleted:    go-server/static/assets/AttachmentView-CFH4hEzI.js
	deleted:    go-server/static/assets/AttachmentView-aZLtbxxg.css
	deleted:    go-server/static/assets/AuditView-BRu9FSBn.css
	deleted:    go-server/static/assets/AuditView-XnOtD3ko.js
	deleted:    go-server/static/assets/DailyHistoryView-BP_fO9HF.css
	deleted:    go-server/static/assets/DailyHistoryView-C8ffhj5C.js
	deleted:    go-server/static/assets/DailyReportDetailView-B4fMKik7.js
	deleted:    go-server/static/assets/DailyReportDetailView-DRGtwlB6.css
	deleted:    go-server/static/assets/DailyReportShell-DqRHOZBI.css
	deleted:    go-server/static/assets/DailyReportShell-PGEPdDKc.js
	deleted:    go-server/static/assets/DailyReportView-3hQ-FjYj.css
	deleted:    go-server/static/assets/DailyReportView-DQ13auP-.js
	deleted:    go-server/static/assets/DashboardView-4NovrSx1.css
	deleted:    go-server/static/assets/DashboardView-CeCUVKqp.js
	deleted:    go-server/static/assets/ExperiencesView-DgW5JF3k.css
	deleted:    go-server/static/assets/ExperiencesView-urYcipe1.js
	deleted:    go-server/static/assets/GasControlView-DSqfhAt0.js
	deleted:    go-server/static/assets/GasControlView-gHNz0d1_.css
	deleted:    go-server/static/assets/InstrumentMeasureView-Ci-F2aWG.css
	deleted:    go-server/static/assets/InstrumentMeasureView-xZOAxtlB.js
	deleted:    go-server/static/assets/IssuesView-C8b57Mdp.css
	deleted:    go-server/static/assets/IssuesView-DtNYCFyo.js
	deleted:    go-server/static/assets/LoginView-CG3hLc3m.css
	deleted:    go-server/static/assets/LoginView-DAswsD5a.js
	deleted:    go-server/static/assets/ProjectDashboard-B8XCx06M.js
	deleted:    go-server/static/assets/ProjectDashboard-BmDd9JFF.css
	deleted:    go-server/static/assets/ProjectLayout-BEDbD0ZL.js
	deleted:    go-server/static/assets/ProjectLayout-BQADk64d.css
	deleted:    go-server/static/assets/ProjectsView-BSRrRwFh.js
	deleted:    go-server/static/assets/ProjectsView-BbeK9OId.css
	deleted:    go-server/static/assets/RFMatchingView-BTZq5DlK.css
	deleted:    go-server/static/assets/RFMatchingView-CUIQ3Q78.js
	deleted:    go-server/static/assets/RunDetailView-BufKNpxO.css
	deleted:    go-server/static/assets/RunDetailView-CVpOHbMW.js
	deleted:    go-server/static/assets/RunListView-6ehT9GYr.css
	deleted:    go-server/static/assets/RunListView-BI9g1z3_.js
	deleted:    go-server/static/assets/SensorsView-gudSUNVP.js
	deleted:    go-server/static/assets/SensorsView-mI1F0_mZ.css
	deleted:    go-server/static/assets/SettingsView-BA5UhtCn.js
	deleted:    go-server/static/assets/SettingsView-mYjUFqhr.css
	deleted:    go-server/static/assets/StepTemplatesView-1D9wI0QP.js
	deleted:    go-server/static/assets/StepTemplatesView-Bx7Sj27f.css
	deleted:    go-server/static/assets/TestDataView-CWFerjpc.css
	deleted:    go-server/static/assets/TestDataView-D6uhEf2v.js
	deleted:    go-server/static/assets/assembly-CLR2rcf4.js
	deleted:    go-server/static/assets/attachments-ijIVDz_q.js
	deleted:    go-server/static/assets/index-D0gTyGT3.css
	deleted:    go-server/static/assets/index-Z85yxgz9.js
	deleted:    go-server/static/assets/instruments-BzJhs5a8.js
	deleted:    go-server/static/assets/issues-B8pVMSia.js
	deleted:    go-server/static/assets/logs-DBdQmoLc.js
	deleted:    go-server/static/assets/runs-C7ZtSwYM.js
	deleted:    go-server/static/assets/stepTemplates-BGFalh8g.css
	deleted:    go-server/static/assets/stepTemplates-BL-szrkj.js
	deleted:    go-server/static/assets/testdata-Dk0C-nZv.js
	modified:   go-server/static/index.html
	modified:   go-server/steptemplates/handler.go
	modified:   go-server/steptemplates/service.go
	modified:   go-server/steptemplates/service_test.go
	modified:   py-agent/serve.py
	modified:   web-ui/src/api/instruments.ts
	modified:   web-ui/src/i18n/en.ts
	modified:   web-ui/src/i18n/zh.ts
	modified:   web-ui/src/views/InstrumentMeasureView.vue

Untracked files:
  (use "git add <file>..." to include in what will be committed)
	.hermes/plans/2026-08-04-work-summary.md
	REVIEW_2026-08-05.md
	REVIEW_CODEX_2026-08-05.md
	REVIEW_CODEX_RECHECK_2026-08-05.md
	go-server/static/assets/AdminUsersView-DBk3QXyC.js
	go-server/static/assets/AdminUsersView-DNcxOYN5.css
	go-server/static/assets/AgentCandidatesView-CSqgg9mn.js
	go-server/static/assets/AgentCandidatesView-fYiOeTFo.css
	go-server/static/assets/AssemblyView-CocMrU1L.css
	go-server/static/assets/AssemblyView-DuSUVaoS.js
	go-server/static/assets/AttachmentView-BtewBlF8.js
	go-server/static/assets/AttachmentView-CylXir0l.css
	go-server/static/assets/AuditView-Bl9Wz6jZ.js
	go-server/static/assets/AuditView-Cge59oAK.css
	go-server/static/assets/DailyHistoryView-DFWu2wC2.css
	go-server/static/assets/DailyHistoryView-Dt6O3Wts.js
	go-server/static/assets/DailyReportDetailView-D9onwq0u.css
	go-server/static/assets/DailyReportDetailView-DLmqSAko.js
	go-server/static/assets/DailyReportShell-BJDzCAJG.js
	go-server/static/assets/DailyReportShell-DqBP4lVL.css
	go-server/static/assets/DailyReportView-CXIHllxD.js
	go-server/static/assets/DailyReportView-D_vEFPLy.css
	go-server/static/assets/DashboardView-DN8JS7NN.css
	go-server/static/assets/DashboardView-DoRDYhJ8.js
	go-server/static/assets/ExperiencesView-B8XElHfg.js
	go-server/static/assets/ExperiencesView-CCN6A0EE.css
	go-server/static/assets/GasControlView-1Znc6PCt.css
	go-server/static/assets/GasControlView-r-oWlI7B.js
	go-server/static/assets/InstrumentMeasureView-DLgj1Voi.js
	go-server/static/assets/InstrumentMeasureView-Dy2hmJnj.css
	go-server/static/assets/IssuesView-BGnvqpoY.css
	go-server/static/assets/IssuesView-BbCQB2Fs.js
	go-server/static/assets/LoginView-CN9iqiLq.css
	go-server/static/assets/LoginView-DgWUGA1g.js
	go-server/static/assets/ProjectDashboard-8IsiLv7u.css
	go-server/static/assets/ProjectDashboard-B3lhXF_w.js
	go-server/static/assets/ProjectLayout-C0SzUdit.css
	go-server/static/assets/ProjectLayout-oOEycowS.js
	go-server/static/assets/ProjectsView-C3yVr9Qp.css
	go-server/static/assets/ProjectsView-CrYqW6pS.js
	go-server/static/assets/RFMatchingView-CCW53U-J.js
	go-server/static/assets/RFMatchingView-CokW0XFf.css
	go-server/static/assets/RunDetailView-BhiTOlcS.css
	go-server/static/assets/RunDetailView-CvyRoPrJ.js
	go-server/static/assets/RunListView-Bb5gveG7.js
	go-server/static/assets/RunListView-CAcRI3rv.css
	go-server/static/assets/SensorsView-Daa-7TyA.css
	go-server/static/assets/SensorsView-YgyKOveA.js
	go-server/static/assets/SettingsView-CXcKJgot.css
	go-server/static/assets/SettingsView-CcRXrmNI.js
	go-server/static/assets/StepTemplatesView-C4Cl6Ep-.css
	go-server/static/assets/StepTemplatesView-CH08iQ2L.js
	go-server/static/assets/TestDataView-Bo80SZ0U.css
	go-server/static/assets/TestDataView-DXQLBNxJ.js
	go-server/static/assets/assembly-CEwFr0hI.js
	go-server/static/assets/attachments-D24D_Ft6.js
	go-server/static/assets/index-CZxi7y-d.js
	go-server/static/assets/index-uIeJdJTm.css
	go-server/static/assets/instruments-BY3EoFsL.js
	go-server/static/assets/issues-BOeSjLP4.js
	go-server/static/assets/logs-vx11FyI3.js
	go-server/static/assets/runs-C8rehhu-.js
	go-server/static/assets/stepTemplates-9kor_NJC.css
	go-server/static/assets/stepTemplates-CegUY34i.js
	go-server/static/assets/testdata-B3DcZWxT.js

no changes added to commit (use "git add" and/or "git commit -a")
 ...1\250\347\231\275\345\220\215\345\215\225.yaml" |  8 ++-
 go-server/instruments/model.go                     |  2 +-
 go-server/instruments/service.go                   | 66 ++++++++++++++++++++-
 go-server/instruments/service_test.go              | 59 ++++++++++++++++--
 go-server/instruments/whitelist_embedded.yaml      |  5 +-
 go-server/static/assets/AdminUsersView-4m67EOip.js |  1 -
 .../static/assets/AdminUsersView-DgHiBl3v.css      |  1 -
 .../static/assets/AgentCandidatesView-9LRfe0IO.js  |  1 -
 .../static/assets/AgentCandidatesView-BexWTry6.css |  1 -
 go-server/static/assets/AssemblyView-Bt4Zawsu.js   |  1 -
 go-server/static/assets/AssemblyView-l0xPRviP.css  |  1 -
 go-server/static/assets/AttachmentView-CFH4hEzI.js |  1 -
 .../static/assets/AttachmentView-aZLtbxxg.css      |  1 -
 go-server/static/assets/AuditView-BRu9FSBn.css     |  1 -
 go-server/static/assets/AuditView-XnOtD3ko.js      |  1 -
 .../static/assets/DailyHistoryView-BP_fO9HF.css    |  1 -
 .../static/assets/DailyHistoryView-C8ffhj5C.js     |  1 -
 .../assets/DailyReportDetailView-B4fMKik7.js       |  1 -
 .../assets/DailyReportDetailView-DRGtwlB6.css      |  1 -
 .../static/assets/DailyReportShell-DqRHOZBI.css    |  1 -
 .../static/assets/DailyReportShell-PGEPdDKc.js     |  1 -
 .../static/assets/DailyReportView-3hQ-FjYj.css     |  1 -
 .../static/assets/DailyReportView-DQ13auP-.js      |  1 -
 go-server/static/assets/DashboardView-4NovrSx1.css |  1 -
 go-server/static/assets/DashboardView-CeCUVKqp.js  |  1 -
 .../static/assets/ExperiencesView-DgW5JF3k.css     |  1 -
 .../static/assets/ExperiencesView-urYcipe1.js      |  1 -
 go-server/static/assets/GasControlView-DSqfhAt0.js |  1 -
 .../static/assets/GasControlView-gHNz0d1_.css      |  1 -
 .../assets/InstrumentMeasureView-Ci-F2aWG.css      |  1 -
 .../assets/InstrumentMeasureView-xZOAxtlB.js       |  2 -
 go-server/static/assets/IssuesView-C8b57Mdp.css    |  1 -
 go-server/static/assets/IssuesView-DtNYCFyo.js     |  1 -
 go-server/static/assets/LoginView-CG3hLc3m.css     |  1 -
 go-server/static/assets/LoginView-DAswsD5a.js      |  1 -
 .../static/assets/ProjectDashboard-B8XCx06M.js     |  1 -
 .../static/assets/ProjectDashboard-BmDd9JFF.css    |  1 -
 go-server/static/assets/ProjectLayout-BEDbD0ZL.js  |  1 -
 go-server/static/assets/ProjectLayout-BQADk64d.css |  1 -
 go-server/static/assets/ProjectsView-BSRrRwFh.js   |  1 -
 go-server/static/assets/ProjectsView-BbeK9OId.css  |  1 -
 .../static/assets/RFMatchingView-BTZq5DlK.css      |  1 -
 go-server/static/assets/RFMatchingView-CUIQ3Q78.js |  1 -
 go-server/static/assets/RunDetailView-BufKNpxO.css |  1 -
 go-server/static/assets/RunDetailView-CVpOHbMW.js  |  1 -
 go-server/static/assets/RunListView-6ehT9GYr.css   |  1 -
 go-server/static/assets/RunListView-BI9g1z3_.js    |  1 -
 go-server/static/assets/SensorsView-gudSUNVP.js    |  1 -
 go-server/static/assets/SensorsView-mI1F0_mZ.css   |  1 -
 go-server/static/assets/SettingsView-BA5UhtCn.js   |  7 ---
 go-server/static/assets/SettingsView-mYjUFqhr.css  |  1 -
 .../static/assets/StepTemplatesView-1D9wI0QP.js    |  1 -
 .../static/assets/StepTemplatesView-Bx7Sj27f.css   |  1 -
 go-server/static/assets/TestDataView-CWFerjpc.css  |  1 -
 go-server/static/assets/TestDataView-D6uhEf2v.js   |  1 -
 go-server/static/assets/assembly-CLR2rcf4.js       |  1 -
 go-server/static/assets/attachments-ijIVDz_q.js    |  1 -
 go-server/static/assets/index-D0gTyGT3.css         |  1 -
 go-server/static/assets/index-Z85yxgz9.js          |  2 -
 go-server/static/assets/instruments-BzJhs5a8.js    |  1 -
 go-server/static/assets/issues-B8pVMSia.js         |  1 -
 go-server/static/assets/logs-DBdQmoLc.js           |  1 -
 go-server/static/assets/runs-C7ZtSwYM.js           |  1 -
 go-server/static/assets/stepTemplates-BGFalh8g.css |  1 -
 go-server/static/assets/stepTemplates-BL-szrkj.js  |  1 -
 go-server/static/assets/testdata-Dk0C-nZv.js       |  1 -
 go-server/static/index.html                        | 28 ++++-----
 go-server/steptemplates/handler.go                 |  3 +
 go-server/steptemplates/service.go                 | 15 +++--
 go-server/steptemplates/service_test.go            | 69 ++++++++++++++++++++++
 py-agent/serve.py                                  |  2 +-
 web-ui/src/api/instruments.ts                      |  3 +-
 web-ui/src/i18n/en.ts                              |  1 +
 web-ui/src/i18n/zh.ts                              |  1 +
 web-ui/src/views/InstrumentMeasureView.vue         | 10 +++-
 75 files changed, 238 insertions(+), 103 deletions(-)

2026-08-05T10:47:46.754603Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:46.754683Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:52.038816Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:52.038893Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:52.293942Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:52.294050Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:52.549665Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:52.549728Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && git diff go-server/steptemplates/service.go go-server/steptemplates/handler.go go-server/steptemplates/service_test.go' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
diff --git a/go-server/steptemplates/handler.go b/go-server/steptemplates/handler.go
index 18cae64..7ff5f85 100644
--- a/go-server/steptemplates/handler.go
+++ b/go-server/steptemplates/handler.go
@@ -179,6 +179,9 @@ func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error)
 		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
 	case errors.Is(err, ErrAgentRejected):
 		common.WriteError(w, r, http.StatusForbidden, "agent_forbidden", err.Error(), nil)
+	case errors.Is(err, ErrUpstream):
+		slog.Error("steptemplates upstream error", "error", err, "request_id", common.GetRequestID(r.Context()))
+		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 生成服务暂时不可用，请稍后再试", nil)
 	default:
 		slog.Error("steptemplates request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
 		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
diff --git a/go-server/steptemplates/service.go b/go-server/steptemplates/service.go
index 5ca3171..2d26ea4 100644
--- a/go-server/steptemplates/service.go
+++ b/go-server/steptemplates/service.go
@@ -25,6 +25,7 @@ var (
 	ErrInvalidInput     = errors.New("请求参数无效")
 	ErrForbidden        = errors.New("当前用户无权执行此操作")
 	ErrAgentRejected    = errors.New("agent 角色不允许执行此操作")
+	ErrUpstream         = errors.New("py-agent 上游服务错误")
 	ErrDuplicateItems   = errors.New("步骤序号重复")
 	ErrDependencyInvalid = errors.New("依赖步骤序号无效")
 )
@@ -101,10 +102,14 @@ func (s *Service) Generate(ctx context.Context, userID, userRole string, req Gen
 		return nil, ErrInvalidInput
 	}
 
+	reqContext := req.Context
+	if reqContext == nil {
+		reqContext = map[string]any{}
+	}
 	payload, err := json.Marshal(map[string]any{
 		"kind":    kind,
 		"prompt":  prompt,
-		"context": req.Context,
+		"context": reqContext,
 	})
 	if err != nil {
 		return nil, err
@@ -119,24 +124,24 @@ func (s *Service) Generate(ctx context.Context, userID, userRole string, req Gen
 
 	resp, err := s.client.Do(httpReq)
 	if err != nil {
-		return nil, fmt.Errorf("py-agent 请求失败: %w", err)
+		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrUpstream, err)
 	}
 	defer resp.Body.Close()
 
 	if resp.StatusCode != http.StatusOK {
 		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
-		return nil, fmt.Errorf("py-agent 返回 %d: %s", resp.StatusCode, string(body))
+		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrUpstream, resp.StatusCode, string(body))
 	}
 
 	var result GenerateResponseData
 	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
 	decoder.DisallowUnknownFields()
 	if err := decoder.Decode(&result); err != nil {
-		return nil, fmt.Errorf("解码 AI 响应失败: %w", err)
+		return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)
 	}
 
 	if result.Status != "ok" && result.Status != "clarify" && result.Status != "rejected" {
-		return nil, fmt.Errorf("AI 返回无效状态: %s", result.Status)
+		return nil, fmt.Errorf("%w: AI 返回无效状态: %s", ErrUpstream, result.Status)
 	}
 
 	if result.Status == "ok" {
diff --git a/go-server/steptemplates/service_test.go b/go-server/steptemplates/service_test.go
index e726221..98117db 100644
--- a/go-server/steptemplates/service_test.go
+++ b/go-server/steptemplates/service_test.go
@@ -1,6 +1,13 @@
 package steptemplates
 
 import (
+	"context"
+	"encoding/json"
+	"errors"
+	"io"
+	"net/http"
+	"net/http/httptest"
+	"strings"
 	"testing"
 )
 
@@ -124,4 +131,66 @@ func TestRequireWriteAccess(t *testing.T) {
 	})
 }
 
+func TestGenerateNilContextSerializedAsEmptyObject(t *testing.T) {
+	var gotBody []byte
+	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		gotBody, _ = io.ReadAll(r.Body)
+		w.Header().Set("Content-Type", "application/json")
+		_, _ = w.Write([]byte(`{"status":"ok","name_suggestion":"t","model":"m","steps":[{"name":"a","step_order":1}]}`))
+	}))
+	defer server.Close()
+
+	svc := NewService(nil, nil)
+	svc.ConfigurePlanner(server.URL, "token")
+
+	_, err := svc.Generate(context.Background(), "user-1", "member", GenerateRequest{
+		Kind:    "assembly",
+		Prompt:  "装一个靶室",
+		Context: nil,
+	})
+	if err != nil {
+		t.Fatalf("Generate failed: %v", err)
+	}
+	var payload map[string]any
+	if err := json.Unmarshal(gotBody, &payload); err != nil {
+		t.Fatalf("unmarshal payload: %v", err)
+	}
+	ctxValue, ok := payload["context"]
+	if !ok {
+		t.Fatal("payload missing context key")
+	}
+	ctxMap, ok := ctxValue.(map[string]any)
+	if !ok {
+		t.Fatalf("context should be an object, got %T (%v)", ctxValue, ctxValue)
+	}
+	if len(ctxMap) != 0 {
+		t.Fatalf("expected empty context, got %v", ctxMap)
+	}
+	if strings.Contains(string(gotBody), `"context":null`) {
+		t.Fatalf("context must not be null: %s", gotBody)
+	}
+}
+
+func TestGenerateUpstreamErrorMarked(t *testing.T) {
+	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		w.WriteHeader(http.StatusBadRequest)
+		_, _ = w.Write([]byte(`{"error":"bad_request"}`))
+	}))
+	defer server.Close()
+
+	svc := NewService(nil, nil)
+	svc.ConfigurePlanner(server.URL, "token")
+
+	_, err := svc.Generate(context.Background(), "user-1", "member", GenerateRequest{
+		Kind:   "experiment",
+		Prompt: "做一次束流实验",
+	})
+	if err == nil {
+		t.Fatal("expected error")
+	}
+	if !errors.Is(err, ErrUpstream) {
+		t.Fatalf("expected ErrUpstream, got %v", err)
+	}
+}
+
 func intPtr(v int) *int { return &v }

exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && git diff go-server/instruments/model.go go-server/instruments/service.go go-server/instruments/service_test.go go-server/instruments/whitelist_embedded.yaml "docs/仪器白名单.yaml"' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
diff --git "a/docs/\344\273\252\345\231\250\347\231\275\345\220\215\345\215\225.yaml" "b/docs/\344\273\252\345\231\250\347\231\275\345\220\215\345\215\225.yaml"
index 9a8dd4c..954d38f 100644
--- "a/docs/\344\273\252\345\231\250\347\231\275\345\220\215\345\215\225.yaml"
+++ "b/docs/\344\273\252\345\231\250\347\231\275\345\220\215\345\215\225.yaml"
@@ -1,5 +1,5 @@
 # 仪器控制白名单
-version: "2026-07-21-v3"
+version: "2026-08-05-v4"
 policy:
   module_boundary: "instrument service only accepts command names from this whitelist; raw SCPI is forbidden"
   yellow_requires:
@@ -160,6 +160,10 @@ e5063a:
           continuous: "INIT1:CONT?"
         failure_action: "mark_instrument_needs_check_and_alert"
       returns: "complex_s11"
+      result_parser:
+        type: sweep_xy_complex
+        x_label: "频率 (Hz)"
+        y_label: "S11 (dB)"
 
     - name: take_screenshot
       description: "保存仪器屏幕截图"
@@ -217,6 +221,8 @@ hioki_im3536:
       risk: green
       scpi: "MEASure?"
       returns: [Z_ohm, theta_deg, param3, param4]
+      result_parser:
+        type: single_value
 
     - name: self_test
       description: "自检"
diff --git a/go-server/instruments/model.go b/go-server/instruments/model.go
index 61bb8d7..f4d6b6c 100755
--- a/go-server/instruments/model.go
+++ b/go-server/instruments/model.go
@@ -106,7 +106,7 @@ type SCPIConnection struct {
 
 // ResultParserConfig describes how to parse a command's raw response.
 type ResultParserConfig struct {
-	Type   string `yaml:"type" json:"type"` // "sweep_xy" | "single_value"
+	Type   string `yaml:"type" json:"type"` // "sweep_xy" | "sweep_xy_complex" | "single_value"
 	XLabel string `yaml:"x_label,omitempty" json:"x_label,omitempty"`
 	YLabel string `yaml:"y_label,omitempty" json:"y_label,omitempty"`
 	Regex  string `yaml:"regex,omitempty" json:"regex,omitempty"`
diff --git a/go-server/instruments/service.go b/go-server/instruments/service.go
index 3c0cc49..e0682e9 100755
--- a/go-server/instruments/service.go
+++ b/go-server/instruments/service.go
@@ -199,11 +199,21 @@ func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult,
 			Type: "sweep_xy", Points: points,
 			XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
 		}, nil
+	case "sweep_xy_complex":
+		// VNA（如 E5063A）扫频响应：第一行 SDATA 复数数组（re,im 对），
+		// 第二行 FREQ 频率轴数组；配对后取幅度换算 dB。
+		return parseSweepXYComplex(def, response)
 	case "single_value":
-		match := firstFloatRegex.FindString(response)
-		if match == "" {
+		loc := firstFloatRegex.FindStringIndex(response)
+		if loc == nil {
 			return nil, fmt.Errorf("响应中未找到数值")
 		}
+		match := response[loc[0]:loc[1]]
+		// 防御性检查：匹配串后紧跟 '.'（如输入 "1.2.3"）说明不是合法的单个数值，
+		// 直接拒绝，避免 ParseFloat 静默截断为 1.2。
+		if loc[1] < len(response) && response[loc[1]] == '.' {
+			return nil, fmt.Errorf("无法解析数值 %q", response)
+		}
 		val, err := strconv.ParseFloat(match, 64)
 		if err != nil {
 			return nil, fmt.Errorf("无法解析数值 %q", match)
@@ -213,6 +223,58 @@ func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult,
 	return nil, nil
 }
 
+// minMagnitude 防止 log10(0) 产生 -Inf（JSON 无法表示）。magnitude 低于此值时钳位，对应 -240 dB。
+const minMagnitude = 1e-12
+
+// parseSweepXYComplex parses a two-line VNA sweep response: the first line is the
+// SDATA complex array (re,im pairs) and the second line is the FREQ axis array.
+// Points are emitted as (frequency, 20*log10|s|) in dB.
+func parseSweepXYComplex(def *CommandDef, response string) (*ParsedResult, error) {
+	sections := make([]string, 0, 2)
+	for _, line := range strings.Split(response, "\n") {
+		if line = strings.TrimSpace(line); line != "" {
+			sections = append(sections, line)
+		}
+	}
+	if len(sections) != 2 {
+		return nil, fmt.Errorf("扫频响应应为两行（复数数据 + 频率轴），实际 %d 行", len(sections))
+	}
+	complexData, err := splitFloatList(sections[0])
+	if err != nil || len(complexData) == 0 || len(complexData)%2 != 0 {
+		return nil, fmt.Errorf("无法解析复数数据段 %q", sections[0])
+	}
+	freqs, err := splitFloatList(sections[1])
+	if err != nil || len(freqs) == 0 {
+		return nil, fmt.Errorf("无法解析频率轴数据段 %q", sections[1])
+	}
+	if len(complexData) != 2*len(freqs) {
+		return nil, fmt.Errorf("复数数据点数 (%d) 与频率轴点数 (%d) 不匹配", len(complexData)/2, len(freqs))
+	}
+	points := make([]Point, 0, len(freqs))
+	for i, freq := range freqs {
+		magnitude := math.Hypot(complexData[2*i], complexData[2*i+1])
+		points = append(points, Point{X: freq, Y: 20 * math.Log10(math.Max(magnitude, minMagnitude))})
+	}
+	return &ParsedResult{
+		Type: "sweep_xy", Points: points,
+		XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
+	}, nil
+}
+
+// splitFloatList parses a comma-separated list of floats; scientific notation is supported.
+func splitFloatList(line string) ([]float64, error) {
+	parts := strings.Split(line, ",")
+	values := make([]float64, 0, len(parts))
+	for _, part := range parts {
+		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
+		if err != nil {
+			return nil, err
+		}
+		values = append(values, value)
+	}
+	return values, nil
+}
+
 // NewSCPIConnection opens a TCP connection to a SCPI instrument.
 func NewSCPIConnection(addr, terminator string) (*SCPIConnection, error) {
 	if addr == "" {
diff --git a/go-server/instruments/service_test.go b/go-server/instruments/service_test.go
index 9522f5e..f05debe 100755
--- a/go-server/instruments/service_test.go
+++ b/go-server/instruments/service_test.go
@@ -3,26 +3,72 @@ package instruments
 import (
 	"context"
 	"encoding/json"
+	"math"
 	"net/http"
 	"net/http/httptest"
+	"strings"
 	"testing"
 )
 
 func TestParseResultSweepXY(t *testing.T) {
+	svc := NewServiceWithGateway("http://unused")
+	// 旧版 "x,y;x,y" 单行格式仍需兼容；regex 须支持科学计数法
+	def := &CommandDef{ResultParser: &ResultParserConfig{
+		Type: "sweep_xy", XLabel: "频率 (Hz)", YLabel: "S11 (dB)",
+		Regex: `(?P<points>(?:[+-]?[\d.]+(?:[eE][+-]?\d+)?,[+-]?[\d.]+(?:[eE][+-]?\d+)?(?:;|$))+)`,
+	}}
+	parsed, err := svc.ParseResult(def, "noise 1.0E+06,-1.05E+01;+2.0E+06,-2.025E+01;3.0E+06,-1.5E+01; tail")
+	if err != nil {
+		t.Fatalf("ParseResult returned error: %v", err)
+	}
+	if parsed.Type != "sweep_xy" || len(parsed.Points) != 3 || parsed.XLabel == "" || parsed.YLabel == "" {
+		t.Fatalf("unexpected parsed result: %+v", parsed)
+	}
+	if parsed.Points[0] != (Point{X: 1e6, Y: -10.5}) || parsed.Points[2] != (Point{X: 3e6, Y: -15.0}) {
+		t.Fatalf("unexpected points: %+v", parsed.Points)
+	}
+}
+
+func TestParseResultSweepXYComplex(t *testing.T) {
 	svc := NewServiceWithGateway("http://unused")
 	def, err := GetCommand("e5063a", "trigger_single")
 	if err != nil || def.ResultParser == nil {
 		t.Fatalf("trigger_single missing result_parser: def=%+v err=%v", def, err)
 	}
-	parsed, err := svc.ParseResult(def, "noise 1000000,-10.5;2000000,-20.25;3000000,-15.0; tail")
+	// E5063A 实际响应：第一行 SDATA 复数数组（re,im 对），第二行 FREQ 频率轴，均为科学计数法
+	response := "+1.00000000E-01,+0.00000000E+00,+7.07106781E-01,-7.07106781E-01\n" +
+		"+1.00000000E+06,+2.00000000E+06"
+	parsed, err := svc.ParseResult(def, response)
 	if err != nil {
 		t.Fatalf("ParseResult returned error: %v", err)
 	}
-	if parsed.Type != "sweep_xy" || len(parsed.Points) != 3 || parsed.XLabel == "" || parsed.YLabel == "" {
+	if parsed.Type != "sweep_xy" || len(parsed.Points) != 2 || parsed.XLabel == "" || parsed.YLabel == "" {
 		t.Fatalf("unexpected parsed result: %+v", parsed)
 	}
-	if parsed.Points[0] != (Point{X: 1000000, Y: -10.5}) || parsed.Points[2] != (Point{X: 3000000, Y: -15.0}) {
-		t.Fatalf("unexpected points: %+v", parsed.Points)
+	// 点 1: |0.1| = 0.1 → -20 dB；点 2: |0.7071+j0.7071| ≈ 1 → ≈0 dB
+	// 两个 dB 断言统一使用同一容差常量
+	const epsilon = 1e-6
+	if parsed.Points[0].X != 1e6 || math.Abs(parsed.Points[0].Y-(-20)) > epsilon {
+		t.Fatalf("unexpected point 0: %+v", parsed.Points[0])
+	}
+	if parsed.Points[1].X != 2e6 || math.Abs(parsed.Points[1].Y) > epsilon {
+		t.Fatalf("unexpected point 1: %+v", parsed.Points[1])
+	}
+}
+
+func TestParseResultSweepXYComplexMismatch(t *testing.T) {
+	svc := NewServiceWithGateway("http://unused")
+	def, err := GetCommand("e5063a", "trigger_single")
+	if err != nil {
+		t.Fatalf("GetCommand: %v", err)
+	}
+	// 复数数据 2 点，频率轴 3 点 → 必须报错
+	if _, err := svc.ParseResult(def, "1.0E-01,0,2.0E-01,0\n1.0E+06,2.0E+06,3.0E+06"); err == nil {
+		t.Fatal("expected error for mismatched complex/frequency lengths")
+	}
+	// 单行响应（缺频率轴）→ 必须报错
+	if _, err := svc.ParseResult(def, "1.0E-01,0,2.0E-01,0"); err == nil {
+		t.Fatal("expected error for single-section response")
 	}
 }
 
@@ -62,6 +108,11 @@ func TestParseResultRejectsMalformedSweep(t *testing.T) {
 	if _, err := svc.ParseResult(def, "no numeric data here"); err == nil {
 		t.Fatal("expected error for unparseable sweep response")
 	}
+	// 两行响应但内容非数字 → splitFloatList 失败，应报"无法解析复数数据段"
+	if _, err := svc.ParseResult(def, "abc,def\nghi,jkl"); err == nil ||
+		!strings.Contains(err.Error(), "无法解析复数数据段") {
+		t.Fatalf("expected splitFloatList parse error, got %v", err)
+	}
 }
 
 func TestInterpretValidatesAgentCandidate(t *testing.T) {
diff --git a/go-server/instruments/whitelist_embedded.yaml b/go-server/instruments/whitelist_embedded.yaml
index b8ddf92..954d38f 100755
--- a/go-server/instruments/whitelist_embedded.yaml
+++ b/go-server/instruments/whitelist_embedded.yaml
@@ -1,5 +1,5 @@
 # 仪器控制白名单
-version: "2026-07-21-v3"
+version: "2026-08-05-v4"
 policy:
   module_boundary: "instrument service only accepts command names from this whitelist; raw SCPI is forbidden"
   yellow_requires:
@@ -161,10 +161,9 @@ e5063a:
         failure_action: "mark_instrument_needs_check_and_alert"
       returns: "complex_s11"
       result_parser:
-        type: sweep_xy
+        type: sweep_xy_complex
         x_label: "频率 (Hz)"
         y_label: "S11 (dB)"
-        regex: "(?P<points>(?:[\\d.]+,[\\d.-]+(?:;|$))+)"
 
     - name: take_screenshot
       description: "保存仪器屏幕截图"

2026-08-05T10:47:55.235765Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:55.235850Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:47:55.660622Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:47:55.660688Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:00.506811Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:00.506872Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:00.760887Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:00.760954Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:01.018395Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:01.018489Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && sed -n '180,300p' go-server/instruments/service.go" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}
			parts := strings.Split(segment, ",")
			if len(parts) != 2 {
				return nil, fmt.Errorf("无法解析数据点 %q", segment)
			}
			x, errX := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			y, errY := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if errX != nil || errY != nil {
				return nil, fmt.Errorf("无法解析数据点 %q", segment)
			}
			points = append(points, Point{X: x, Y: y})
		}
		if len(points) == 0 {
			return nil, fmt.Errorf("响应中未找到扫频数据")
		}
		return &ParsedResult{
			Type: "sweep_xy", Points: points,
			XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
		}, nil
	case "sweep_xy_complex":
		// VNA（如 E5063A）扫频响应：第一行 SDATA 复数数组（re,im 对），
		// 第二行 FREQ 频率轴数组；配对后取幅度换算 dB。
		return parseSweepXYComplex(def, response)
	case "single_value":
		loc := firstFloatRegex.FindStringIndex(response)
		if loc == nil {
			return nil, fmt.Errorf("响应中未找到数值")
		}
		match := response[loc[0]:loc[1]]
		// 防御性检查：匹配串后紧跟 '.'（如输入 "1.2.3"）说明不是合法的单个数值，
		// 直接拒绝，避免 ParseFloat 静默截断为 1.2。
		if loc[1] < len(response) && response[loc[1]] == '.' {
			return nil, fmt.Errorf("无法解析数值 %q", response)
		}
		val, err := strconv.ParseFloat(match, 64)
		if err != nil {
			return nil, fmt.Errorf("无法解析数值 %q", match)
		}
		return &ParsedResult{Type: "single_value", Value: &val}, nil
	}
	return nil, nil
}

// minMagnitude 防止 log10(0) 产生 -Inf（JSON 无法表示）。magnitude 低于此值时钳位，对应 -240 dB。
const minMagnitude = 1e-12

// parseSweepXYComplex parses a two-line VNA sweep response: the first line is the
// SDATA complex array (re,im pairs) and the second line is the FREQ axis array.
// Points are emitted as (frequency, 20*log10|s|) in dB.
func parseSweepXYComplex(def *CommandDef, response string) (*ParsedResult, error) {
	sections := make([]string, 0, 2)
	for _, line := range strings.Split(response, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			sections = append(sections, line)
		}
	}
	if len(sections) != 2 {
		return nil, fmt.Errorf("扫频响应应为两行（复数数据 + 频率轴），实际 %d 行", len(sections))
	}
	complexData, err := splitFloatList(sections[0])
	if err != nil || len(complexData) == 0 || len(complexData)%2 != 0 {
		return nil, fmt.Errorf("无法解析复数数据段 %q", sections[0])
	}
	freqs, err := splitFloatList(sections[1])
	if err != nil || len(freqs) == 0 {
		return nil, fmt.Errorf("无法解析频率轴数据段 %q", sections[1])
	}
	if len(complexData) != 2*len(freqs) {
		return nil, fmt.Errorf("复数数据点数 (%d) 与频率轴点数 (%d) 不匹配", len(complexData)/2, len(freqs))
	}
	points := make([]Point, 0, len(freqs))
	for i, freq := range freqs {
		magnitude := math.Hypot(complexData[2*i], complexData[2*i+1])
		points = append(points, Point{X: freq, Y: 20 * math.Log10(math.Max(magnitude, minMagnitude))})
	}
	return &ParsedResult{
		Type: "sweep_xy", Points: points,
		XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
	}, nil
}

// splitFloatList parses a comma-separated list of floats; scientific notation is supported.
func splitFloatList(line string) ([]float64, error) {
	parts := strings.Split(line, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

// NewSCPIConnection opens a TCP connection to a SCPI instrument.
func NewSCPIConnection(addr, terminator string) (*SCPIConnection, error) {
	if addr == "" {
		return nil, fmt.Errorf("SCPI address is required")
	}
	if terminator == "" {
		return nil, fmt.Errorf("SCPI terminator is required")
	}
	const timeout = 10 * time.Second
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to SCPI instrument %s: %w", addr, err)
	}
	return &SCPIConnection{addr: addr, terminator: terminator, timeout: timeout, conn: conn}, nil
}

// Send writes each newline- or semicolon-delimited command and reads query responses.
func (c *SCPIConnection) Send(cmd string) (string, error) {
	if c == nil || c.conn == nil {
		return "", fmt.Errorf("SCPI connection is closed")
	}

	var responses []string

exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && git diff py-agent/serve.py web-ui/src/api/instruments.ts web-ui/src/views/InstrumentMeasureView.vue web-ui/src/i18n/zh.ts web-ui/src/i18n/en.ts' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
diff --git a/py-agent/serve.py b/py-agent/serve.py
index 42a7411..69aaaef 100644
--- a/py-agent/serve.py
+++ b/py-agent/serve.py
@@ -78,7 +78,7 @@ def create_app(interpreter, planner, token):
                 raise ValueError("request too large")
             kind = data.get("kind")
             prompt = data.get("prompt")
-            context = data.get("context", {})
+            context = data.get("context") or {}
             if kind not in {"assembly", "experiment"}:
                 raise ValueError("kind is invalid")
             if not isinstance(prompt, str) or not prompt.strip() or len(prompt) > 4000:
diff --git a/web-ui/src/api/instruments.ts b/web-ui/src/api/instruments.ts
index 730e7bb..36710ee 100644
--- a/web-ui/src/api/instruments.ts
+++ b/web-ui/src/api/instruments.ts
@@ -34,6 +34,7 @@ export type WhitelistCommand = {
   timeout_ms?: number
   params?: Record<string, CommandParamDef>
   returns?: unknown
+  result_parser?: { type: string; x_label?: string; y_label?: string; regex?: string }
 }
 
 export type CommandResult = {
@@ -120,7 +121,7 @@ export function executeCommandWithMeta(id: string, command: string, params: Reco
   return requestWithMeta<CommandResult>({ url: `/instruments/${id}/commands`, method: 'POST', data: { command, params } })
 }
 
-// 只读解析接口，不需要 Idempotency-Key；解析失败（parse_failed）由调用方静默处理
+// 只读解析接口，不需要 Idempotency-Key；解析失败（parse_failed）由调用方决定如何提示
 export function parseResult(id: string, command: string, response: string) {
   return request<ParsedResult | null>({ url: `/instruments/${id}/parse-result`, method: 'POST', data: { command, response } })
 }
diff --git a/web-ui/src/i18n/en.ts b/web-ui/src/i18n/en.ts
index a3fcba0..35c8f6b 100644
--- a/web-ui/src/i18n/en.ts
+++ b/web-ui/src/i18n/en.ts
@@ -118,6 +118,7 @@ export default {
     confirmWrite: 'This is a write command (yellow) that will change the instrument state. Confirm execution?',
     writeConfirm: 'Write Confirmation',
     executeSuccess: 'Command {name} executed successfully',
+    parseFailed: 'Failed to parse the result: {reason}. Showing the raw response.',
     projectsLoadFailed: 'Failed to load project list',
     runsLoadFailed: 'Failed to load runs list',
     pleaseSelectProject: 'Please select a project',
diff --git a/web-ui/src/i18n/zh.ts b/web-ui/src/i18n/zh.ts
index 2c41824..154efdc 100644
--- a/web-ui/src/i18n/zh.ts
+++ b/web-ui/src/i18n/zh.ts
@@ -110,6 +110,7 @@ export default {
     confirmWrite: '确认执行吗？',
     writeConfirm: '写入确认',
     executeSuccess: '命令 {name} 执行成功',
+    parseFailed: '结果解析失败：{reason}，仅显示原始响应',
     projectsLoadFailed: '项目列表加载失败',
     runsLoadFailed: '批次列表加载失败',
     pleaseSelectProject: '请选择项目',
diff --git a/web-ui/src/views/InstrumentMeasureView.vue b/web-ui/src/views/InstrumentMeasureView.vue
index 4bb14d4..a1664d9 100644
--- a/web-ui/src/views/InstrumentMeasureView.vue
+++ b/web-ui/src/views/InstrumentMeasureView.vue
@@ -556,12 +556,18 @@ function clearCmdResult() {
   destroyChart()
 }
 
-// 解析失败（命令未配置 result_parser 返回 null，响应无法解析返回 400）不影响原始结果展示，静默忽略
+// 命令未配置 result_parser 时后端返回 null，属正常情况；配置了 parser 但解析失败时必须让用户知道
 async function parseExecution(instrumentId: string, command: string, response?: string): Promise<ParsedResult | null> {
   if (!response) return null
   try {
     return await parseResult(instrumentId, command, response)
-  } catch {
+  } catch (err) {
+    const reason = err instanceof Error ? err.message : String(err)
+    console.warn(`[instruments] parse-result failed: command=${command} reason=${reason}`)
+    const hasParser = Boolean(whitelist.value.find((c) => c.name === command)?.result_parser)
+    if (hasParser) {
+      ElMessage.warning(t('instrument.parseFailed', { reason }))
+    }
     return null
   }
 }

2026-08-05T10:48:03.737765Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:03.737827Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:04.681673Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed, when Client(HttpRequest(HttpRequest("http/request failed: error sending request for url (https://chatgpt.com/backend-api/ps/mcp)")))
2026-08-05T10:48:05.933026Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:05.933077Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:09.014149Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:09.014222Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:09.275084Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:09.275172Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:09.531962Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:09.532073Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:16.208092Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:16.208182Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && rg -n "firstFloatRegex|var .*Regex|FindStringIndex" go-server/instruments/*.go' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
go-server/instruments/service.go:151:// firstFloatRegex matches the first floating-point number in a response.
go-server/instruments/service.go:152:var firstFloatRegex = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)
go-server/instruments/service.go:207:		loc := firstFloatRegex.FindStringIndex(response)

exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && sed -n '140,200p' go-server/instruments/service.go && sed -n '1,80p' go-server/instruments/service.go" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
	return candidate, nil
}

// NewServiceWithGateway creates a Service for tests and explicit callers.
func NewServiceWithGateway(gateway string) *Service {
	return &Service{
		client:  &http.Client{Timeout: 15 * time.Second},
		gateway: normalizeHTTPBase(gateway),
	}
}

// firstFloatRegex matches the first floating-point number in a response.
var firstFloatRegex = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

// ParseResult parses a raw command response according to the command's result_parser
// whitelist config. Returns (nil, nil) when the command has no parser configured.
func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult, error) {
	if def.ResultParser == nil {
		return nil, nil
	}
	switch def.ResultParser.Type {
	case "sweep_xy":
		// response contains "freq1,val1;freq2,val2;..."
		data := strings.TrimSpace(response)
		if def.ResultParser.Regex != "" {
			re, err := regexp.Compile(def.ResultParser.Regex)
			if err != nil {
				return nil, fmt.Errorf("invalid result_parser regex: %w", err)
			}
			match := re.FindStringSubmatch(data)
			if match == nil {
				return nil, fmt.Errorf("响应中未找到扫频数据")
			}
			data = match[0]
			if idx := re.SubexpIndex("points"); idx >= 0 && idx < len(match) && match[idx] != "" {
				data = match[idx]
			}
		}
		points := make([]Point, 0)
		for _, segment := range strings.Split(data, ";") {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}
			parts := strings.Split(segment, ",")
			if len(parts) != 2 {
				return nil, fmt.Errorf("无法解析数据点 %q", segment)
			}
			x, errX := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			y, errY := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if errX != nil || errY != nil {
				return nil, fmt.Errorf("无法解析数据点 %q", segment)
			}
			points = append(points, Point{X: x, Y: y})
		}
		if len(points) == 0 {
			return nil, fmt.Errorf("响应中未找到扫频数据")
		}
		return &ParsedResult{
			Type: "sweep_xy", Points: points,
			XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
package instruments

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Service wraps calls to the EPICS gateway.
type Service struct {
	client         *http.Client
	gateway        string
	interpretURL   string
	interpretToken string
}

const (
	piezoSetpointMin = 0.0
	piezoSetpointMax = 100.0
)

var (
	ErrGasCellPermission = fmt.Errorf("gascell control permission denied")
	ErrGasCellGateway    = fmt.Errorf("gascell gateway error")
)

// NewService creates an instruments Service with a bounded HTTP client.
func NewService() (*Service, error) {
	gateway := os.Getenv("EPICS_GATEWAY_ADDR")
	if gateway == "" {
		return nil, fmt.Errorf("EPICS_GATEWAY_ADDR is required")
	}
	svc := NewServiceWithGateway(gateway)
	svc.interpretURL = strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	tokenPath := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE")
	if tokenPath != "" {
		token, err := os.ReadFile(filepath.Clean(tokenPath))
		if err != nil {
			return nil, fmt.Errorf("read py-agent internal token: %w", err)
		}
		svc.interpretToken = strings.TrimSpace(string(token))
	}
	return svc, nil
}

// ConfigureInterpreter sets the internal translator endpoint; used by startup and tests.
func (s *Service) ConfigureInterpreter(url, token string) {
	s.interpretURL = strings.TrimRight(url, "/")
	s.interpretToken = token
}

type interpretResponse struct {
	Status        string         `json:"status"`
	Command       string         `json:"command"`
	Params        map[string]any `json:"params"`
	Confidence    float64        `json:"confidence"`
	Explanation   string         `json:"explanation"`
	Question      string         `json:"question"`
	Reason        string         `json:"reason"`
	PromptVersion string         `json:"prompt_version"`
	Model         string         `json:"model"`
}

// Interpret calls the internal no-tool translator, then validates and renders its candidate locally.
func (s *Service) Interpret(ctx context.Context, instrumentID string, req NLCommandRequest) (*NLCommandCandidate, error) {
	if s.interpretURL == "" || s.interpretToken == "" {
		return nil, fmt.Errorf("py-agent interpreter is not configured")

2026-08-05T10:48:19.286130Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:19.286225Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:24.562355Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:24.562472Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:24.820157Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:24.820202Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:25.090159Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:25.090235Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:26.480146Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:26.480248Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:34.889660Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed, when Client(HttpRequest(HttpRequest("http/request failed: error sending request for url (https://chatgpt.com/backend-api/ps/mcp)")))
2026-08-05T10:48:36.747490Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:36.747585Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && git diff go-server/instruments/service.go | sed -n '1,120p' && echo \"=====HIOKI embedded=====\" && sed -n '200,235p' go-server/instruments/whitelist_embedded.yaml" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
diff --git a/go-server/instruments/service.go b/go-server/instruments/service.go
index 3c0cc49..e0682e9 100755
--- a/go-server/instruments/service.go
+++ b/go-server/instruments/service.go
@@ -199,11 +199,21 @@ func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult,
 			Type: "sweep_xy", Points: points,
 			XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
 		}, nil
+	case "sweep_xy_complex":
+		// VNA（如 E5063A）扫频响应：第一行 SDATA 复数数组（re,im 对），
+		// 第二行 FREQ 频率轴数组；配对后取幅度换算 dB。
+		return parseSweepXYComplex(def, response)
 	case "single_value":
-		match := firstFloatRegex.FindString(response)
-		if match == "" {
+		loc := firstFloatRegex.FindStringIndex(response)
+		if loc == nil {
 			return nil, fmt.Errorf("响应中未找到数值")
 		}
+		match := response[loc[0]:loc[1]]
+		// 防御性检查：匹配串后紧跟 '.'（如输入 "1.2.3"）说明不是合法的单个数值，
+		// 直接拒绝，避免 ParseFloat 静默截断为 1.2。
+		if loc[1] < len(response) && response[loc[1]] == '.' {
+			return nil, fmt.Errorf("无法解析数值 %q", response)
+		}
 		val, err := strconv.ParseFloat(match, 64)
 		if err != nil {
 			return nil, fmt.Errorf("无法解析数值 %q", match)
@@ -213,6 +223,58 @@ func (s *Service) ParseResult(def *CommandDef, response string) (*ParsedResult,
 	return nil, nil
 }
 
+// minMagnitude 防止 log10(0) 产生 -Inf（JSON 无法表示）。magnitude 低于此值时钳位，对应 -240 dB。
+const minMagnitude = 1e-12
+
+// parseSweepXYComplex parses a two-line VNA sweep response: the first line is the
+// SDATA complex array (re,im pairs) and the second line is the FREQ axis array.
+// Points are emitted as (frequency, 20*log10|s|) in dB.
+func parseSweepXYComplex(def *CommandDef, response string) (*ParsedResult, error) {
+	sections := make([]string, 0, 2)
+	for _, line := range strings.Split(response, "\n") {
+		if line = strings.TrimSpace(line); line != "" {
+			sections = append(sections, line)
+		}
+	}
+	if len(sections) != 2 {
+		return nil, fmt.Errorf("扫频响应应为两行（复数数据 + 频率轴），实际 %d 行", len(sections))
+	}
+	complexData, err := splitFloatList(sections[0])
+	if err != nil || len(complexData) == 0 || len(complexData)%2 != 0 {
+		return nil, fmt.Errorf("无法解析复数数据段 %q", sections[0])
+	}
+	freqs, err := splitFloatList(sections[1])
+	if err != nil || len(freqs) == 0 {
+		return nil, fmt.Errorf("无法解析频率轴数据段 %q", sections[1])
+	}
+	if len(complexData) != 2*len(freqs) {
+		return nil, fmt.Errorf("复数数据点数 (%d) 与频率轴点数 (%d) 不匹配", len(complexData)/2, len(freqs))
+	}
+	points := make([]Point, 0, len(freqs))
+	for i, freq := range freqs {
+		magnitude := math.Hypot(complexData[2*i], complexData[2*i+1])
+		points = append(points, Point{X: freq, Y: 20 * math.Log10(math.Max(magnitude, minMagnitude))})
+	}
+	return &ParsedResult{
+		Type: "sweep_xy", Points: points,
+		XLabel: def.ResultParser.XLabel, YLabel: def.ResultParser.YLabel,
+	}, nil
+}
+
+// splitFloatList parses a comma-separated list of floats; scientific notation is supported.
+func splitFloatList(line string) ([]float64, error) {
+	parts := strings.Split(line, ",")
+	values := make([]float64, 0, len(parts))
+	for _, part := range parts {
+		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
+		if err != nil {
+			return nil, err
+		}
+		values = append(values, value)
+	}
+	return values, nil
+}
+
 // NewSCPIConnection opens a TCP connection to a SCPI instrument.
 func NewSCPIConnection(addr, terminator string) (*SCPIConnection, error) {
 	if addr == "" {
=====HIOKI embedded=====

# ====================
# Hioki IM3536 — LCR 表
# ====================
hioki_im3536:
  name: "Hioki IM3536"
  type: "lcr_meter"
  interface: "lan_scpi"
  ip: "10.51.12.101"
  port: 3500
  terminator: "\r\n"

  commands:
    # --- 🟢 只读 ---
    - name: identify
      description: "读取仪器标识"
      risk: green
      scpi: "*IDN?"

    - name: measure_single
      description: "读取当前测量值 (Z, θ, 参数3, 参数4)"
      risk: green
      scpi: "MEASure?"
      returns: [Z_ohm, theta_deg, param3, param4]
      result_parser:
        type: single_value

    - name: self_test
      description: "自检"
      risk: green
      scpi: "*TST?"

    - name: read_frequency
      description: "查询当前频率设置"
      risk: green
      scpi: "FREQuency?"

exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && sed -n '100,140p' go-server/steptemplates/service.go && sed -n '1,60p' go-server/steptemplates/service.go" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" || len(prompt) > 4000 {
		return nil, ErrInvalidInput
	}

	reqContext := req.Context
	if reqContext == nil {
		reqContext = map[string]any{}
	}
	payload, err := json.Marshal(map[string]any{
		"kind":    kind,
		"prompt":  prompt,
		"context": reqContext,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.plannerURL+"/v1/step-plan", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.plannerToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrUpstream, resp.StatusCode, string(body))
	}

	var result GenerateResponseData
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)
package steptemplates

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrTemplateNotFound = errors.New("模板不存在")
	ErrInvalidInput     = errors.New("请求参数无效")
	ErrForbidden        = errors.New("当前用户无权执行此操作")
	ErrAgentRejected    = errors.New("agent 角色不允许执行此操作")
	ErrUpstream         = errors.New("py-agent 上游服务错误")
	ErrDuplicateItems   = errors.New("步骤序号重复")
	ErrDependencyInvalid = errors.New("依赖步骤序号无效")
)

type stepRepo interface {
	Create(template *StepTemplate, items []StepTemplateItem) (*StepTemplate, error)
	GetByID(id string) (*StepTemplate, error)
	GetTemplateWithItems(id string) (*StepTemplate, []StepTemplateItem, error)
	List(kind, query string, page, perPage int) ([]StepTemplate, int, error)
	Update(id string, req UpdateTemplateRequest) (*StepTemplate, error)
	ReplaceItems(templateID string, items []StepTemplateItem) error
	SoftDelete(id string) error
}

type Service struct {
	repo            stepRepo
	db              *sql.DB
	client          *http.Client
	plannerURL      string
	plannerToken    string
	rlMu            sync.Mutex
	rlCalls         map[string][]time.Time
}

func NewService(repo stepRepo, db *sql.DB) *Service {
	svc := &Service{
		repo:    repo,
		db:      db,
		client:  &http.Client{Timeout: 60 * time.Second},
		rlCalls: map[string][]time.Time{},
	}
	return svc

2026-08-05T10:48:42.374637Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:42.374719Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:47.021493Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:47.021558Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:47.662718Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:47.662796Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:48.240508Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:48.240554Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:48.505751Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:48.505843Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && sed -n '140,200p' go-server/steptemplates/service.go && echo \"=====HANDLER=====\" && sed -n '160,215p' go-server/steptemplates/handler.go" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
		return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)
	}

	if result.Status != "ok" && result.Status != "clarify" && result.Status != "rejected" {
		return nil, fmt.Errorf("%w: AI 返回无效状态: %s", ErrUpstream, result.Status)
	}

	if result.Status == "ok" {
		if err := validateSteps(result.Steps); err != nil {
			return nil, fmt.Errorf("AI 生成的步骤校验失败: %w", err)
		}
		result.Steps = reorderSteps(result.Steps)
	}

	return &result, nil
}

func (s *Service) Create(userID, userRole string, req CreateTemplateRequest) (*StepTemplate, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if err := s.requireWriteAccess(userRole, userID, nil); err != nil {
		return nil, err
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	items := reorderAndNormalizeItems(req.Items)
	template := &StepTemplate{
		Name:         strings.TrimSpace(req.Name),
		Kind:         strings.TrimSpace(req.Kind),
		Description:  strings.TrimSpace(req.Description),
		SourcePrompt: strings.TrimSpace(req.SourcePrompt),
		AIGenerated:  req.AIGenerated,
		CreatedBy:    &userID,
	}
	return s.repo.Create(template, items)
}

func (s *Service) GetByID(id, userID, userRole string) (*StepTemplate, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	template, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, ErrTemplateNotFound
	}
	return template, nil
}

func (s *Service) List(userRole string, kind, query string, page, perPage int) (*ListResult, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
=====HANDLER=====
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func decode(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTemplateNotFound):
		common.WriteError(w, r, http.StatusNotFound, "template_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	case errors.Is(err, ErrAgentRejected):
		common.WriteError(w, r, http.StatusForbidden, "agent_forbidden", err.Error(), nil)
	case errors.Is(err, ErrUpstream):
		slog.Error("steptemplates upstream error", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 生成服务暂时不可用，请稍后再试", nil)
	default:
		slog.Error("steptemplates request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && git diff go-server/static/index.html | head -50 && echo \"=====\" && git status --porcelain go-server/static/ | grep -v '"'^ M'"\\|"'^D '"' | head -5" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
diff --git a/go-server/static/index.html b/go-server/static/index.html
index e4d3260..6d3aaeb 100644
--- a/go-server/static/index.html
+++ b/go-server/static/index.html
@@ -1,18 +1,18 @@
-<!doctype html>
-<html lang="zh-CN">
-  <head>
-    <meta charset="UTF-8" />
-    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
-    <meta name="theme-color" content="#1f6f8b" />
-    <title>HIAF Lab System</title>
-    <script type="module" crossorigin src="/assets/index-Z85yxgz9.js"></script>
+<!doctype html>
+<html lang="zh-CN">
+  <head>
+    <meta charset="UTF-8" />
+    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
+    <meta name="theme-color" content="#1f6f8b" />
+    <title>HIAF Lab System</title>
+    <script type="module" crossorigin src="/assets/index-CZxi7y-d.js"></script>
     <link rel="modulepreload" crossorigin href="/assets/vendor-FE4yofNv.js">
     <link rel="modulepreload" crossorigin href="/assets/vendor-icons-PcFktes8.js">
     <link rel="modulepreload" crossorigin href="/assets/vendor-element-CVF8W9ZL.js">
     <link rel="stylesheet" crossorigin href="/assets/vendor-element-COT_jIef.css">
-    <link rel="stylesheet" crossorigin href="/assets/index-D0gTyGT3.css">
-  </head>
-  <body>
-    <div id="app"></div>
-  </body>
-</html>
+    <link rel="stylesheet" crossorigin href="/assets/index-uIeJdJTm.css">
+  </head>
+  <body>
+    <div id="app"></div>
+  </body>
+</html>
=====
 D go-server/static/assets/AdminUsersView-4m67EOip.js
 D go-server/static/assets/AdminUsersView-DgHiBl3v.css
 D go-server/static/assets/AgentCandidatesView-9LRfe0IO.js
 D go-server/static/assets/AgentCandidatesView-BexWTry6.css
 D go-server/static/assets/AssemblyView-Bt4Zawsu.js

2026-08-05T10:48:53.742977Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:53.743086Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:57.306159Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:57.306245Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:59.020860Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:59.020921Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:59.282911Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:59.282992Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:48:59.540137Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:48:59.540225Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:07.578028Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:07.578237Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:12.265503Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed, when Client(HttpRequest(HttpRequest("http/request failed: error sending request for url (https://chatgpt.com/backend-api/ps/mcp)")))
exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && sed -n '60,110p' py-agent/serve.py" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
                str(data.get("instrument_id", ""))[:128], str(data.get("instrument_name", ""))[:256],
                commands, user_input, history,
            )
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "interpretation_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    async def step_plan(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
                raise ValueError("request too large")
            kind = data.get("kind")
            prompt = data.get("prompt")
            context = data.get("context") or {}
            if kind not in {"assembly", "experiment"}:
                raise ValueError("kind is invalid")
            if not isinstance(prompt, str) or not prompt.strip() or len(prompt) > 4000:
                raise ValueError("prompt is invalid")
            if not isinstance(context, dict):
                raise ValueError("context is invalid")
            result = planner.plan(kind, prompt.strip(), context)
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "planning_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    return Starlette(routes=[
        Route("/health", health),
        Route("/v1/interpret", interpret, methods=["POST"]),
        Route("/v1/step-plan", step_plan, methods=["POST"]),
    ])


if __name__ == "__main__":
    api_key = os.getenv("DEEPSEEK_API_KEY")
    if not api_key:
        raise RuntimeError("DEEPSEEK_API_KEY environment variable is not set")
    app = create_app(InstrumentInterpreter(api_key), StepPlanner(api_key), read_token())
    uvicorn.run(app, host="0.0.0.0", port=8001)

exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && ls go-server/static/assets/ | rg "index-|vendor-" && echo "=====diff whitelists=====" && diff go-server/instruments/whitelist_embedded.yaml "docs/仪器白名单.yaml" && echo IDENTICAL' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
index-CZxi7y-d.js
index-uIeJdJTm.css
vendor-FE4yofNv.js
vendor-element-COT_jIef.css
vendor-element-CVF8W9ZL.js
vendor-icons-PcFktes8.js
=====diff whitelists=====
IDENTICAL

2026-08-05T10:49:16.979567Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:16.979628Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:17.852272Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:17.852354Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:22.254250Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:22.254311Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:22.530422Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:22.530504Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:22.794561Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:22.794661Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
codex
白名单两份一致、前端静态产物与 index.html 引用一致。现在跑 Go 测试确认。
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system/go-server && go build ./... && go test ./instruments/... ./steptemplates/... 2>&1 | tail -20' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 676ms:
ok  	github.com/zhu571/hiaf-lab-system/go-server/instruments	(cached)
ok  	github.com/zhu571/hiaf-lab-system/go-server/steptemplates	(cached)

2026-08-05T10:49:27.832479Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:27.832573Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:28.126343Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:28.126406Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:33.109297Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:33.109351Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:33.814154Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:33.814196Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:34.079433Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:34.079509Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system/go-server && go test -count=1 ./... 2>&1 | tail -25' in /home/zhuhaofan/hiaf-lab-system
2026-08-05T10:49:38.397456Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:38.397518Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:47.082092Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed, when Client(HttpRequest(HttpRequest("http/request failed: error sending request for url (https://chatgpt.com/backend-api/ps/mcp)")))
2026-08-05T10:49:48.672652Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:48.672754Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
 succeeded in 16067ms:
?   	github.com/zhu571/hiaf-lab-system/go-server/audit	[no test files]
?   	github.com/zhu571/hiaf-lab-system/go-server/cmd/seed-agent	[no test files]
ok  	github.com/zhu571/hiaf-lab-system/go-server	0.011s
ok  	github.com/zhu571/hiaf-lab-system/go-server/agent	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/assembly	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/attachments	0.007s
?   	github.com/zhu571/hiaf-lab-system/go-server/sensors	[no test files]
ok  	github.com/zhu571/hiaf-lab-system/go-server/auth	0.351s
ok  	github.com/zhu571/hiaf-lab-system/go-server/cmd/update-runner	0.007s
ok  	github.com/zhu571/hiaf-lab-system/go-server/common	0.008s
ok  	github.com/zhu571/hiaf-lab-system/go-server/experiences	0.005s
ok  	github.com/zhu571/hiaf-lab-system/go-server/instruments	0.011s
ok  	github.com/zhu571/hiaf-lab-system/go-server/issues	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/logs	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/middleware	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/notify	0.005s
ok  	github.com/zhu571/hiaf-lab-system/go-server/projects	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/rfmatch	0.002s
ok  	github.com/zhu571/hiaf-lab-system/go-server/runs	0.003s
ok  	github.com/zhu571/hiaf-lab-system/go-server/steptemplates	0.004s
ok  	github.com/zhu571/hiaf-lab-system/go-server/system	15.135s

2026-08-05T10:49:52.274857Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:52.274925Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:57.556800Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:57.556853Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:57.812887Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:57.812944Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:58.066121Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:58.066236Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:49:58.954991Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:49:58.955091Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:09.286933Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:09.286984Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && gofmt -l go-server/steptemplates/ go-server/instruments/ && echo "---gofmt clean---" && git show HEAD:go-server/instruments/whitelist_embedded.yaml | rg -n -A3 "measure_single"' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
go-server/steptemplates/model.go
go-server/steptemplates/service.go
---gofmt clean---
220:    - name: measure_single
221-      description: "读取当前测量值 (Z, θ, 参数3, 参数4)"
222-      risk: green
223-      scpi: "MEASure?"

2026-08-05T10:50:12.252046Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:12.252101Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:17.535991Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:17.536096Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:17.786343Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:17.786426Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:18.043427Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:18.043496Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:19.555222Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:19.555285Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc 'cd /home/zhuhaofan/hiaf-lab-system && git show HEAD:go-server/steptemplates/service.go | gofmt -l /dev/stdin 2>/dev/null; gofmt -d go-server/steptemplates/service.go | head -40' in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
/dev/stdin
diff go-server/steptemplates/service.go.orig go-server/steptemplates/service.go
--- go-server/steptemplates/service.go.orig
+++ go-server/steptemplates/service.go
@@ -21,12 +21,12 @@
 )
 
 var (
-	ErrTemplateNotFound = errors.New("模板不存在")
-	ErrInvalidInput     = errors.New("请求参数无效")
-	ErrForbidden        = errors.New("当前用户无权执行此操作")
-	ErrAgentRejected    = errors.New("agent 角色不允许执行此操作")
-	ErrUpstream         = errors.New("py-agent 上游服务错误")
-	ErrDuplicateItems   = errors.New("步骤序号重复")
+	ErrTemplateNotFound  = errors.New("模板不存在")
+	ErrInvalidInput      = errors.New("请求参数无效")
+	ErrForbidden         = errors.New("当前用户无权执行此操作")
+	ErrAgentRejected     = errors.New("agent 角色不允许执行此操作")
+	ErrUpstream          = errors.New("py-agent 上游服务错误")
+	ErrDuplicateItems    = errors.New("步骤序号重复")
 	ErrDependencyInvalid = errors.New("依赖步骤序号无效")
 )
 
@@ -41,13 +41,13 @@
 }
 
 type Service struct {
-	repo            stepRepo
-	db              *sql.DB
-	client          *http.Client
-	plannerURL      string
-	plannerToken    string
-	rlMu            sync.Mutex
-	rlCalls         map[string][]time.Time
+	repo         stepRepo
+	db           *sql.DB
+	client       *http.Client
+	plannerURL   string
+	plannerToken string
+	rlMu         sync.Mutex
+	rlCalls      map[string][]time.Time

2026-08-05T10:50:20.894454Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:20.894501Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:22.411546Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed, when Client(HttpRequest(HttpRequest("http/request failed: error sending request for url (https://chatgpt.com/backend-api/ps/mcp)")))
2026-08-05T10:50:26.189552Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:26.189638Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:26.441572Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:26.441655Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:26.693588Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:26.693657Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:29.830579Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:29.830680Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
exec
/bin/bash -lc "cd /home/zhuhaofan/hiaf-lab-system && rg -n \"parse-result|parse_failed|ParseResult\" go-server/instruments/handler.go | head && echo \"=====\" && sed -n '/func (h \\*Handler) parseResult/,/"'^}/p'"' go-server/instruments/handler.go" in /home/zhuhaofan/hiaf-lab-system
 succeeded in 0ms:
108:// ParseResult handles POST /api/v1/instruments/{id}/parse-result.
111:func (h *Handler) ParseResult(w http.ResponseWriter, r *http.Request) {
130:	parsed, err := h.svc.ParseResult(def, req.Response)
132:		common.WriteError(w, r, http.StatusBadRequest, "parse_failed", err.Error(), nil)
=====

2026-08-05T10:50:34.051516Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:34.051593Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:34.324539Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:34.324601Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:34.586532Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:34.586594Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:34.891571Z ERROR codex_models_manager::manager: failed to refresh available models: stream disconnected before completion: failed to decode models response: missing field `models` at line 1 column 156; body: {"object":"list","data":[{"id":"deepseek-v4-flash","object":"model","owned_by":"deepseek"},{"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek"}]}
2026-08-05T10:50:39.433631Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:39.433700Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:39.684880Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:39.685030Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:39.944022Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:39.944140Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
2026-08-05T10:50:40.200569Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:40.200630Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
codex
## 复审结论

**🔴 硬伤**
- 无。

**🟡 建议改**
- 无新增。仅一处可选：`TestGenerateUpstreamErrorMarked` 只覆盖了非 200 路径，解码失败路径（`service.go:140`）未覆盖——两处是同一 `%w` 模式，可不补；若想补齐，在同一测试里加一个返回合法 JSON 但解码失败（如非法字节）的用例即可。
- 顺带说明（非本轮引入）：`steptemplates/service.go` 的 `var` 块对齐不符合 gofmt（`HEAD` 版本同样不通过，CI 未卡此关），本轮新增 `ErrUpstream` 行沿用了已有风格，不阻塞上线。

**🟢 好设计**
- 修复 1 正确：`%w` 同时包装 `ErrUpstream` 与底层错误，`errors.Is` 链完整，与 127/133/144 行归类一致；`writeError` 新增 `ErrUpstream → 502 upstream_error` 分支，用户侧文案友好，且 `slog.Error` 保留完整错误链。
- 修复 2 准确：`// "sweep_xy" | "sweep_xy_complex" | "single_value"` 与 `ParseResult` 的 switch 分支一一对应。
- 全局复查无新问题：`py-agent` 的 `or {}` 与 Go 侧 `map[string]any{}` 一致；前端 `parseFailed` 仅在有 parser 配置时告警，zh/en key 对齐；`docs/仪器白名单.yaml` 与 `whitelist_embedded.yaml` 内容完全一致（v4）；`index.html` 引用的新 hash 资产均存在。
- `go test -count=1 ./...` 全绿（含 `instruments`、`steptemplates`）。

**结论：YES，可上线。**
tokens used
35,033
## 复审结论

**🔴 硬伤**
- 无。

**🟡 建议改**
- 无新增。仅一处可选：`TestGenerateUpstreamErrorMarked` 只覆盖了非 200 路径，解码失败路径（`service.go:140`）未覆盖——两处是同一 `%w` 模式，可不补；若想补齐，在同一测试里加一个返回合法 JSON 但解码失败（如非法字节）的用例即可。
- 顺带说明（非本轮引入）：`steptemplates/service.go` 的 `var` 块对齐不符合 gofmt（`HEAD` 版本同样不通过，CI 未卡此关），本轮新增 `ErrUpstream` 行沿用了已有风格，不阻塞上线。

**🟢 好设计**
- 修复 1 正确：`%w` 同时包装 `ErrUpstream` 与底层错误，`errors.Is` 链完整，与 127/133/144 行归类一致；`writeError` 新增 `ErrUpstream → 502 upstream_error` 分支，用户侧文案友好，且 `slog.Error` 保留完整错误链。
- 修复 2 准确：`// "sweep_xy" | "sweep_xy_complex" | "single_value"` 与 `ParseResult` 的 switch 分支一一对应。
- 全局复查无新问题：`py-agent` 的 `or {}` 与 Go 侧 `map[string]any{}` 一致；前端 `parseFailed` 仅在有 parser 配置时告警，zh/en key 对齐；`docs/仪器白名单.yaml` 与 `whitelist_embedded.yaml` 内容完全一致（v4）；`index.html` 引用的新 hash 资产均存在。
- `go test -count=1 ./...` 全绿（含 `instruments`、`steptemplates`）。

**结论：YES，可上线。**
2026-08-05T10:50:50.496774Z ERROR codex_login::auth::manager: Failed to refresh token: 403 Forbidden: {"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}
2026-08-05T10:50:50.496840Z ERROR codex_login::auth::manager: Failed to refresh token: Failed to refresh token: 403 Forbidden: Country, region, or territory not supported
