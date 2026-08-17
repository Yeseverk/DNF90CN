# DNF90CN 开发环境

## 进入 90CN

### 客户端

将以下五个文件替换到客户端根目录：

- `DNF.exe`
- `90CN.dll`
- `90CNLua.dll`
- `ijl15.dll`
- `ijl15_real.dll`

### 服务端

1. 双击运行 `START.bat`。
2. 编辑 `DNF90-source-oneclick\runtime\config\instance.json`，将倒数第三行的 `directory` 改为客户端路径，例如：

```json
"directory": "E:\\DNF_90\\DNF_90_CN\\地下城与勇士"
```

3. 保存配置，双击运行 `STATUS.bat`。
4. 右键以管理员身份运行 `LOGIN.bat`。
5. 注册账号并登录。
6. 创建第一个角色并进入游戏。

客户端与服务端的 `Script.pvf`、`.lst` 注册和对应服务端实现必须保持一致。
