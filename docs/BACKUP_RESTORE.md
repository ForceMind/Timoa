# 备份、恢复与演练

## 一致性备份

- 快照使用 SQLite `VACUUM INTO`（一致性快照，不复制正在写入的主库文件）。
- 包内容：`xiaozhang.db`（快照）+ `attachments/`（附件）+ `manifest.json`（格式版本、每文件 sha256、恢复说明）。
- 生成后即刻校验快照：integrity_check、外键、全库分录借贷平衡。未完成包不写正式文件名（`.part` 临时文件，完整后改名）。
- 加密：设置 `XIAOZHANG_BACKUP_KEY` 后用 age（scrypt 口令）加密为 `.tar.gz.age`。**口令不随包保存**；未设置密钥时明确为未加密状态，不生成假加密文件。
- 频率：每日自动（`XIAOZHANG_BACKUP_TIME`，默认 03:17）+ 手动 `xiaozhang backup` 或管理端「立即备份」（需重新输入密码）。
- 保留：默认保留最近 14 个；磁盘不足也不会删掉最后一个好备份。
- 本机备份不能应对整机丢失：管理端可下载加密包到其他设备。

## 恢复（需要停服的明确授权流程）

```bash
systemctl stop xiaozhang            # 或 docker compose stop
xiaozhang restore -yes -data /var/lib/xiaozhang /path/to/xiaozhang-YYYYMMDD-HHMMSS.tar.gz[.age]
systemctl start xiaozhang
```

恢复流程保证：

1. 解包到独立临时目录：防路径穿越、格式版本、逐文件校验和、数据库完整性、外键、借贷平衡、附件存在性——任一失败**不碰现有数据**。
2. 校验通过后才执行：先把当前数据做一次安全备份（可回退），再原子替换。
3. 恢复后：撤销全部会话；`restore_generation` 更新——旧客户端必须重新对齐状态，离线队列不能盲目重放（阶段 5 同步协议读取该世代）。

## 演练记录（自动化测试）

`go test ./internal/backup/`（TestBackupRestoreDrill，T42/T43）：

- 建账 → 消费 500 + 退款 200 + 附件 → 备份 → 独立目录校验恢复 → 概览口径、账户余额、退款链、附件逐项一致 ✓
- 篡改一字节的包校验失败，不覆盖好数据 ✓

最近一次执行：2026-09-16（全部通过）。
