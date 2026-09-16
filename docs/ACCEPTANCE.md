# 验收记录（T01–T50）

图例：✅ 已实现且自动化测试通过 · 🧪 已实现，已手动/curl 验证 · ⏳ 未实现 · ❌ 失败待修

验证命令：`go test ./...`（2026-09-15 全绿）；浏览器走查记录于各阶段提交。

## 冲账与账务

| 编号 | 用例 | 状态 | 证据 |
| --- | --- | --- | --- |
| T01 | 消费 500 退 200 → 净支出 300，收入不增 | ✅ | TestT01_RefundReducesNetExpense |
| T02 | 多次退款到上限拒绝；并发退款不超额 | ✅ | TestT02_RefundCapAndConcurrency（双 goroutine 并发） |
| T03 | 拆分 食品200/日用品100 退日用品60 → 200/40 | ✅ | TestT03_SplitRefundPerCategory |
| T04 | 信用卡消费后还款：费用一次、负债减少、溢缴为负 | ✅ | TestCreditCardDebtAndRepayment |
| T05 | 自有账户互转不计收支、净资产不变 | ✅ | TestTransferIsNotIncomeOrExpense |
| T06 | 聚餐 500 自担 100 代付 400，回款后费用仍 100 | ✅ | TestT06_SharedBillWithReceivable |
| T07 | 多次部分回款结清、超额结算拒绝 | ✅ | TestT07_PartialSettlements |
| T08 | 支出转待报销再收回：无现金流、无重复收入 | ✅ | TestT08_ReclassThenSettle |
| T09 | 混合自担/代付的退款与回款不重复结算 | ✅ | TestT09_MixedRefundAndSettlement |
| T10 | 收入 1000 退 200 → 净收入 800 | ✅ | TestT10_IncomeRefund |
| T11 | 350 更正为 35：费用 35、审计完整、无虚假退款 | ✅ | TestT11_Correction |
| T12 | 作废不重复冲正；有下游关联阻止作废 | ✅ | TestT12_VoidRules |
| T13 | 3 月消费 4 月退款：三口径分别正确 | ✅ | TestT13_CrossMonthBases |
| T14 | 借入/借出/还本/收本/利息/押金归属 | ✅ | TestT14_BorrowLendRepayDeposit |
| T15 | 房贷本息、理财赎回本金收益不混算 | ✅ | TestT15_LoanAndRedeem |
| T16 | 期初/对账差额/早于基准日导入不重复影响 | ⏳ | 阶段 4 导入 |
| T17 | 小数输入、非法金额、超范围、聚合溢出、溢缴款 | ✅ | TestParseYuan/TestCheckedAddOverflow/TestCreditCardDebtAndRepayment/TestValidation |

## 幂等、同步与离线

| 编号 | 用例 | 状态 | 证据 |
| --- | --- | --- | --- |
| T18 | 相同 operation_id 重试不重复；异内容冲突 | ✅ | TestIdempotentPosting |
| T19 | 响应丢失后重试只产生一笔 | ✅ | TestIdempotentPosting（重放路径） |
| T20–T24 | 离线队列、多标签、版本冲突、会话与依赖保护 | 🧪 | T20 ✅（浏览器实测：断网入队→同步失败保留→恢复后入账且仅一次）；T18/T19 服务端幂等已 ✅；T23 会话过期暂停同步、世代不一致 409 已实测；T22 版本冲突（base_version）与 T24 依赖排序为后续增强 |

## 周期、推荐

| 编号 | 用例 | 状态 |
| --- | --- | --- |
| T25–T32 | 月末闰年、农历、实例防重、推荐不污染账务 | 🧪 | T25 月末不漂移/2-29 策略、T27 关联确认与部分付款、T28 跳过/延后/停用/重启补查、T29 共享不重复均已 ✅（recurrence_test.go）；农历/法定工作日/预测口径待阶段 4 |

## 统计与口径

| 编号 | 用例 | 状态 | 证据 |
| --- | --- | --- | --- |
| T33 | 分类父子不双算、笔数口径 | ⏳ | 阶段 4（两层分类模型已就位） |
| T34 | 零收入、负净费用、空数据、上期为零 | 🧪 | 上期为零不显示较上月（前端 compareBadge 返回 null）；负净支出用例见 T13 |
| T35 | 报表/图表/列表/导出口径一致 | 🧪 | 全部读同一服务端定义（Overview/CategoryNets）；导出未实现 |

## 模板、导入导出、权限、安全

| 编号 | 用例 | 状态 |
| --- | --- | --- |
| T36–T37 | 预制模板幂等种子、自定义模板 | 🧪 | T36 ✅（TestSeedLibraryIdempotent：115 模板/两级分类/幂等重跑）；T37 自定义模板停用/置顶已实现，导入导出待阶段 4 |
| T38–T39 | 导入映射、去重、待处理 | ⏳ 阶段 4 |
| T40 | 普通成员权限 | 🧪 服务端 requireAdmin/会员校验已实现；成员管理未实现 |
| T41 | 路径穿越、公式注入、非法附件 | ⏳ 阶段 4 |
| T42–T44 | 备份恢复演练、升级保护 | 🧪 | T42/T43 ✅（internal/backup 演练测试：独立目录恢复逐项一致、篡改包拒绝）；CLI restore 实测（校验→安全备份→换文件→会话撤销→恢复世代）；T44 客户端队列保护待阶段 5 |
| T45 | 360px/桌面/主题/大字体/键盘 | 🧪 360px 布局、44px 触控区、label 关联已实现；深浅主题未实现 |
| T46 | 全部最多二级 | 🧪 详情/记账均为替换式二级页面 |
| T47 | PWA | 🧪 | Manifest（192/512/maskable 生图图标）+ standalone + SW 应用壳缓存 + 离线回退实测；安装引导按浏览器能力（无假开关）；后台同步不可用设备的降级：回前台/联网/手动触发 |
| T48 | 生产无演示数据、部署配置 | 🧪 生产启动不生成演示数据；Dockerfile/compose/systemd/反代片段/配置示例齐备；本机无 Docker，镜像构建未验证（如实记录）；生产二进制已实测 |
| T49 | 参考设计映射 | 🧪 docs/REFERENCE_RESEARCH.md 许可证实测，深入映射随阶段补充 |
| T50 | 构建/类型检查/测试/生产构建真实执行 | 🧪 每阶段提交前执行 go build/test、tsc、vite build |

性能测试：未执行（阶段 6）。
