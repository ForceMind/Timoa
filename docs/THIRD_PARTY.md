# 第三方许可与素材记录

核验日期：2026-09-15。原则：默认不复制上游业务源码、测试、文案、图片、图标；确需引入时先记录来源、许可与义务。

## 本项目许可证

- 待决定（项目所有者未定）。在决定前不替用户做授权承诺。

## 直接依赖

### Go（见 go.mod / go.sum）

| 依赖 | 版本 | 许可 | 用途 |
| --- | --- | --- | --- |
| modernc.org/sqlite | v1.58.0 | BSD-3-Clause | 纯 Go SQLite 驱动（嵌入 SQLite 3.53.4，经 `sqlite_version()` 实测） |

（golang.org/x/crypto 等后续加入时补充。）

### npm（见 web/package.json / package-lock.json）

| 依赖 | 用途 |
| --- | --- |
| react / react-dom | UI |
| vite / @vitejs/plugin-react / typescript | 构建与类型 |

（ECharts 等后续加入时补充具体版本与许可。）

## 素材

- 图标、字体：尚未引入第三方素材；后续仅使用原创或授权清楚的素材并记录于此。不使用远程字体与第三方 CDN。

## 参考项目（研究用，不进入构建）

actualbudget/actual（MIT）、firefly-iii/firefly-iii（AGPL-3.0）、TNT-Likely/BeeCount（自定义非商业）、jameskokoska/Cashew（GPL-3.0）、moneymanagerex/moneymanagerex（GPL-2.0）、ananthakumaran/paisa（AGPL-3.0）、ellite/Wallos（GPL-3.0）、momentmaker/pancakemaker（MIT）。

详见 docs/REFERENCE_RESEARCH.md。不复制其代码与素材。
