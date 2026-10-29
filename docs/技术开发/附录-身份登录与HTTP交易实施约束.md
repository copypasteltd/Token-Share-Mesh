# 身份登录与HTTP交易实施约束

R004｜沿需求v3.0、NB-R1和完整源码目标实施。本文追加控制面身份流程；资金、设备、模型转发和前端仍须独立完成。34项局部原生用例通过，真实支付宝授权未联调，不以本地签名夹具冒充渠道验收。实际证据见[验证报告](../../dev/tests/evidence/R004-验证报告.md)。

## 1. 编号与来源

新增操作显式登记INT-090—093：createAuthChallenge、createUserSession、getCurrentSession、revokeUserSession；已有INT编号不变。新增DATA-061 auth_sessions、DATA-062 auth_attempts；原60表及001/002迁移不改字节，由003前向建立，当前运行模型共62表。接口权威仍为scripts/白皮书契约数据.py生成的控制OpenAPI。

截至2026-10-30核对：

- [支付宝授权码API](https://opendocs.alipay.com/mini/api/openapi-authorize)：页面动态正文未能直接抓取；以[官方my.getAuthCode说明](https://idocs.alipay.com/miniprogram/miniprogram/mpdev/api_openapi_getauthcode)交叉核对授权码流程。国内应用的实际能力须正式联调。
- [官方OAuth接口参数](https://developer.alibaba.com/docs/api.htm?apiId=1025&docType=4&source=search)：alipay.system.oauth.token通过authorization_code换取身份；这是旧版官方接口资料，不据此声称所有新应用返回同一种主体字段。
- [支付宝官方SDK源码](https://github.com/alipay/alipay-sdk-nodejs-all/blob/master/src/alipay.ts)：核对响应节点原始JSON字节验签路径；本项目独立实现Go RSA2适配，不复制SDK源码或默认跳过验签。
- [官方网页授权流程](https://docs.antom.com/ac/common/obtain_uid)：生成固定授权域名、配置应用及已登记回调；应用场景可用性另核。

## 2. 授权与会话

先创建限时挑战，客户端在内存持有随机verifier，发送其SHA-256挑战值。此为本项目登录绑定，不宣称支付宝支持PKCE参数。WEB另绑定HttpOnly一次性Cookie；MINIAPP不依赖浏览器Cookie。网页授权state必须等于本次challenge_id，回调后由前端将授权码通过POST交给本服务，并清除回调地址中的临时授权码，不将会话凭据放入URL。

兑换先事务认领挑战和授权码HMAC摘要，再在事务外调用固定支付宝HTTPS网关。请求RSA2签名；响应必须对原始response节点字节进行RSA2验签，验证成功后才读取配置指定的OPEN_ID或USER_ID，不在两者间自动回退。身份命名空间绑定应用ID与字段类型；变更身份摘要密钥或字段须显式迁移，不把同一人静默映射到另一账号。

挑战状态PENDING→EXCHANGING→SUCCEEDED/FAILED/UNKNOWN。授权码不明时重新发起授权，不自动重兑原码；失败与未知均不签发会话。数据库仅保存挑战/凭据摘要，原授权码、access_token和refresh_token不落库或进入审计、响应和普通日志。

首次通过核验创建ACTIVE账号并按应用注册政策授予本人BUYER角色；不授予SELLER或后台角色。已有账号的撤销角色不因再次登录恢复，SUSPENDED/CLOSING/CLOSED不由登录复活。每次成功登录提升用户版本，签发有限期会话并使旧会话失效；登出撤销会话并提升版本。业务事务再次核当前用户、角色以及已绑定会话的撤销与期限。

WEB会话只放HttpOnly、SameSite=Strict的主机Cookie，默认Secure；明确的原生loopback开发模式才允许非Secure本机Cookie。MINIAPP会话只在首次响应返回原值，客户端只在运行内存保留，后续通过Authorization发送；这不是SDK调用或付款凭据。刷新/重新启动客户端应重新授权。原值不进入可重放幂等缓存。

## 3. HTTP边界

浏览器Origin使用明确允许值；Host白名单、重复凭据拒绝、有界请求体、精确字段名和重复JSON键拒绝、请求超时、安全错误、无缓存响应和有界限流共同实施。浏览器写操作同时核Origin和会话绑定CSRF，不把CORS或SameSite单独当CSRF验证。小程序Bearer会话与浏览器Cookie不混用；API key仍不能购买或管理本人会话。

会话查询可返回本会话CSRF值及安全档案，浏览器只在内存保存CSRF值。/me的身份状态表示平台核验等级，OAuth登录不等于KYC、卖家收款资格或VERIFIED财务身份。

本轮接入本人档案、密钥管理、市场查询、报价创建/读取及订单创建/读取。列表公开游标附HMAC并绑定主体和筛选范围，不能由客户端修改内部游标伪造。未实现的路由不返回假成功。密钥创建重放按既有契约返回409 SECRET_ALREADY_ISSUED和原key引用，原值只首次返回。

实际13个操作由[嵌入路由登记](../../dev/src/api/internal/http/routes.json)驱动，构建映射时对照权威93操作核方法/路径与operationId；登记并不表示所有Schema约束或产品验收已完成。控制请求体上限64KiB、URI上限2048字节、请求15秒、同时最多32个处理槽。单实例来源/用户默认120次每分钟、匿名授权12次每分钟；4096个有限桶到容量先清过期项，仍满则拒绝。公网或多实例限流另行设计，本阶段只原生本机开发。

市场limit默认20/最多50，密钥列表最多100；重复或未知查询参数、GET/DELETE请求体、压缩控制JSON、大小写别名、未知/重复键均拒绝。游标绑定用户及筛选条件，客户端不能取内部游标跨用户或换筛选复用。Cookie和Bearer、两种会话类别、重复认证头不自动择一。允许跨域的响应仅暴露请求编号、回放标记和重试间隔等安全头；未受信Origin不给通行。

报价和订单仍遵守R003合同及当前资格复核；WAITING_PAYMENT不等于已付款或锁住GPU。HTTP身份实现不打开支付、执行或未获证服务。API命令仅监听本机原生loopback；不增加部署、HTTPS配置或可观测性工程。

## 4. 验证与剩余

使用临时原生PostgreSQL验证挑战单次认领、未知不放行、版本及角色撤销、会话归属、密钥重放，以及本机真实HTTP的Cookie/CSRF/跨用户/畸形请求边界。RSA2网关测试使用进程内生成的测试密钥及明确的合成响应；不能替代真实支付宝小程序、网页授权、应用资质或支付联调。

34项含既有22项回归及新增12项；16并发相同挑战只发生一次兑换及签发。首轮发现空值DTO被接受和测试小时标识错误，两处修正后在全新实例复测；首轮失败事件保留在evidence，不抹掉失败。测试未执行Go race detector、断电恢复或完整角色/终端攻击评审。

仍需真实应用授权、账号恢复/合并政策、会话密钥轮换迁移、全角色矩阵、设备凭据、前端配对与跨端整体测试。80项主验收、NB96场景和真实组合门槛不由本轮局部测试自动通过。

## 5. 客户端衔接顺序

小程序先在内存生成足够随机的verifier→POST /auth/challenges（MINIAPP与SHA-256 challenge）→my.getAuthCode取得新授权码→POST /auth/sessions→仅在内存保存首次access_token。后续请求使用Bearer；读取/auth/session不会重新返回令牌。登出DELETE /auth/session后清内存；丢令牌或进程重启重新授权，不把授权码当登录凭据重放。

Web先生成verifier并创建WEB挑战，保留内存中的challenge_id/verifier并允许本机绑定Cookie；在同一SPA流程接收官方授权回调，核state后立即从地址移除临时码，再POST兑换。当前未实现前端弹窗/回调，因此不把整页跳转后丢失内存verifier视为可用流程；后续优先用受源限制的同源回调窗口向原页面传一次性结果，不用localStorage落会话秘密。兑换后只读Cookie由浏览器管理，前端只保CSRF值，每次写请求带X-CSRF-Token并保留Cookie，GET会话可恢复CSRF值。跨应用嵌入、第三方Cookie和跨设备登录不在本轮自动兼容范围。

交易接入顺序为本人会话→GET /services→POST /quotes→明确展示合同并确认→POST /orders→GET原订单。每个写意图保留稳定Idempotency-Key；不得由客户端传user_id/role或复用API key代购。新建订单仅WAITING_PAYMENT，下一阶段接支付，不显示“已付款”“已接单”或“已开机”。

## R005后续实施

R004的13操作/62表是当轮快照。当前新增本人卖家/同意/挂牌10操作与004前向，23操作、64表；详见[卖家与挂牌](附录-卖家申请与挂牌版本实施约束.md)。BUYER可读本人待审档案和请求退出，仍无挂牌SELLER授权；原双端凭据、CSRF、撤销与合成渠道边界继续适用。
