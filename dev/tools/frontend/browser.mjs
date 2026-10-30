// Native browser -> actual HTTP control service -> disposable PostgreSQL.
// This test-only runner receives synthetic qualification/OAuth from the Go harness.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
const require = createRequire(path.join(root, 'runtime/frontend/workspace/package.json'));
const { chromium } = require('playwright-core');
const target = process.env.P11_UI_TARGET;
const roundID = process.env.P11_EVIDENCE_ROUND || 'R006';
if (!['R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID)) throw new Error('Explicit evidence round required');
const providerRound = ['R009', 'R010', 'R011', 'R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID);
if (!['web', 'miniapp'].includes(target)) throw new Error('Explicit UI target required');
const base = new URL(process.env.P11_UI_BASE);
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1') throw new Error('Disposable loopback harness required');
const browserPath = process.env.P11_BROWSER_PATH || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
].find(existsSync);
if (!browserPath) throw new Error('Existing native Edge/Chrome required');
const output = path.join(root, 'dev/media', roundID === 'R006' ? '' : roundID);
const runtime = path.join(root, 'runtime/frontend/browser', target + '-' + Date.now());
for (const p of [runtime, path.join(output, 'screenshots'), path.join(output, 'recordings')]) mkdirSync(p, { recursive: true });
const temporary = path.join(root, 'runtime/tmp/frontend-browser'); mkdirSync(temporary, { recursive: true });
process.env.TEMP = process.env.TMP = temporary;
const receipt = { round: roundID, target, passed: false, buyer_id: '', order_id: '', browser_closed: false, remaining_owned_tabs: null,
  assertions: [], screenshots: [], recording: null, scope: 'Actual compiled UI/HTTP/DB with synthetic OAuth/qualification/device heartbeats; H5 is not native Alipay validation; no payment, device or inference execution', virtualization_used: false };
const record = item => receipt.assertions.push(item);
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const digest = f => createHash('sha256').update(readFileSync(f)).digest('hex');
let browser, context, page, cdp, stoppedAt;
const errors = [], frames = [], orderRequests = [];
let lostReceipt = false, quoteRequests = 0;
let lostPairing = false, lostPolicy = false, pairingCredential = '';
const pairRequests = [], policyRequests = [];
let releasePairRecovery;
const heldPairRecovery = new Promise(resolve => { releasePairRecovery = resolve; });
try {
  browser = await chromium.launch({ executablePath: browserPath, headless: true, args: ['--disable-extensions', '--no-first-run', '--disable-background-networking'] });
  receipt.browser_version = browser.version();
  context = await browser.newContext({ viewport: target === 'web' ? { width: 1440, height: 1000 } : { width: 390, height: 844 }, deviceScaleFactor: 1, locale: 'zh-CN', reducedMotion: 'reduce', ...(target === 'miniapp' ? { isMobile: true, hasTouch: true } : {}) });
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url());
    if (url.origin !== base.origin) return route.abort();
    if (url.pathname === '/v1/p11/quotes' && request.method() === 'POST') quoteRequests++;
    if (url.pathname === '/v1/p11/orders' && request.method() === 'POST') {
      orderRequests.push({ key: request.headers()['idempotency-key'], body: request.postData() });
      if (!lostReceipt) { lostReceipt = true; const actual = await route.fetch(); assert.equal(actual.status(), 201); return route.abort('failed'); }
    }
    if (providerRound && url.pathname === '/v1/p11/devices/pairing-intents' && request.method() === 'POST') {
      pairRequests.push({ key: request.headers()['idempotency-key'], body: request.postData() });
      if (!lostPairing) { lostPairing = true; const actual = await route.fetch(); assert.equal(actual.status(), 201); return route.abort('failed'); }
      if (['R011', 'R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID) && pairRequests.length === 2) {
        const actual = await route.fetch(); assert.equal(actual.status(), 201);
        await heldPairRecovery; return route.fulfill({ response: actual });
      }
    }
    if (providerRound && url.pathname.endsWith('/resource-policy') && request.method() === 'PATCH') {
      policyRequests.push({ key: request.headers()['idempotency-key'], body: request.postData(), ifMatch: request.headers()['if-match'] });
      if (!lostPolicy) { lostPolicy = true; const actual = await route.fetch(); assert.equal(actual.status(), 200); return route.abort('failed'); }
    }
    return route.continue();
  });
  page = await context.newPage(); page.setDefaultTimeout(12000);
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', async r => {
    if (r.url().endsWith('/v1/p11/auth/sessions') && r.status() === 201) {
      try { receipt.buyer_id = (await r.json()).profile.user_id; } catch { /* Navigation failures are asserted below. */ }
    }
  });
  const host = locator => locator.locator('xpath=ancestor-or-self::*[self::button or self::taro-button-core][1]');
  const button = name => host(page.getByText(name, { exact: true }).filter({ visible: true }).last());
  const navigation = name => target === 'miniapp'
    ? page.locator(`.bottom-nav taro-button-core[aria-label="${name}"]`).filter({ visible: true }).last()
    : button(name);
  const input = name => page.locator(`input[aria-label="${name}"], [aria-label="${name}"] input`).filter({ visible: true }).first();
  const visible = async text => { await page.getByText(text, { exact: false }).filter({ visible: true }).first().waitFor({ state: 'visible' }); };
  const readyMarket = async () => {
    const resource = page.getByText('摩托罗拉 X30', { exact: true }).filter({ visible: true });
    const unavailable = page.getByText('服务暂不可用，请稍后重试。', { exact: true }).filter({ visible: true });
    for (let attempt = 0; attempt < 3; attempt++) {
      await resource.or(unavailable).first().waitFor({ state: 'visible' });
      if (await resource.count()) return;
      assert(attempt < 2, 'Market read remains unavailable after bounded explicit refreshes');
      // A concurrent fixture heartbeat can conflict with the real repeatable-read
      // catalog snapshot. Exercise the existing UI refresh; never retry a write.
      await button(target === 'web' ? '刷新资源 ↻' : '清除筛选并刷新').click();
      receipt.market_read_refreshes = (receipt.market_read_refreshes || 0) + 1;
    }
  };
  const readableButton = async locator => {
    const result = await locator.evaluate(el => {
      const style = getComputedStyle(el);
      const rgba = value => {
        const parts = value.match(/[\d.]+/g)?.map(Number);
        if (!parts || parts.length < 3) throw new Error('Unsupported solid button color: ' + value);
        return [parts[0], parts[1], parts[2], parts[3] ?? 1];
      };
      let background, ancestor = el;
      while (ancestor) {
        const candidate = rgba(getComputedStyle(ancestor).backgroundColor);
        if (candidate[3] === 1) { background = candidate; break; }
        if (candidate[3] !== 0) throw new Error('Contrast check needs an opaque solid background');
        ancestor = ancestor.parentElement;
      }
      if (!background) throw new Error('No opaque button background');
      const foreground = rgba(style.color);
      const composite = foreground.slice(0, 3).map((v, i) => v * foreground[3] + background[i] * (1 - foreground[3]));
      const luminance = rgb => rgb.slice(0, 3).map(v => { const c = v / 255; return c <= .04045 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4; }).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
      const a = luminance(composite), b = luminance(background);
      const range = document.createRange(); range.selectNodeContents(el);
      const boxes = [...range.getClientRects()];
      return { text: el.textContent?.trim(), contrast: (Math.max(a, b) + .05) / (Math.min(a, b) + .05), rendered: boxes.some(box => box.width > 0 && box.height > 0) && style.visibility === 'visible' && style.display !== 'none' };
    });
    assert(result.text && result.rendered && result.contrast >= 4.5, 'Button label must render with at least 4.5:1 text contrast: ' + JSON.stringify(result));
  };
  const shot = async (name, deviceCard) => {
    await page.evaluate(() => document.fonts.ready);
    if (deviceCard) await deviceCard.evaluate(el => el.scrollIntoView({ block: 'start', inline: 'nearest', behavior: 'instant' }));
    await page.waitForFunction(() => {
      const active = Array.from(document.querySelectorAll('.taro_page')).filter(el => getComputedStyle(el).display !== 'none').at(-1);
      if (!active) return true;
      const box = active.getBoundingClientRect();
      return Math.abs(box.left) < 1 && Math.abs(box.width - innerWidth) < 1;
    });
    await pause(180);
    const relative = `dev/media/${roundID === 'R006' ? '' : roundID + '/'}screenshots/${name}.png`, f = path.join(root, relative);
    // Keep the original viewport: oversized element screenshots resize the H5
    // scroll container and clip its content. The recording covers the full form.
    await page.screenshot({ path: f });
    receipt.screenshots.push({ path: relative, sha256: digest(f), viewport: page.viewportSize(), actual_ui: true, capture: deviceCard ? 'ACTUAL_PAGE_VIEWPORT_AT_DEVICE' : 'ACTUAL_PAGE_VIEWPORT' });
  };
  const choose = async id => {
    const row = page.locator(`[data-listing-id="${id}"]`);
    await row.waitFor({ state: 'visible' });
    await host(row.getByText(/查看服务|已选择/).last()).click();
  };
  await page.goto(base.origin + '/' + target + '/', { waitUntil: 'networkidle' });
  await button(target === 'web' ? '支付宝授权登录 →' : '支付宝授权登录').click();
  await readyMarket(); await visible('RTX 4090 24GB');
  assert(receipt.buyer_id, 'Actual session response must supply the buyer');
  assert(!page.url().includes('auth_code=') && !page.url().includes('state='), 'OAuth return parameters removed');
  const persisted = await page.evaluate(() => JSON.stringify(Object.fromEntries(Object.keys(sessionStorage).map(k => [k, sessionStorage.getItem(k)]))));
  assert(!persisted.includes('access_token') && !persisted.includes('verifier'), 'No token/verifier left in browser storage');
  record('Signed synthetic OAuth -> real HttpOnly session; one-time proof removed from URL/storage');
  if (target === 'miniapp') {
    for (const filter of await page.locator('.mode-button').all()) await readableButton(filter);
    record('Rendered enabled mode labels meet 4.5:1 text contrast after the loading/disabled transition');
  }
  await shot(target + '-market');
  cdp = await context.newCDPSession(page);
  cdp.on('Page.screencastFrame', async event => {
    const filename = `frame-${String(frames.length).padStart(6, '0')}.jpg`;
    frames.push({ filename, timestamp: performance.now() / 1000 });
    writeFileSync(path.join(runtime, filename), Buffer.from(event.data, 'base64'));
    try { await cdp.send('Page.screencastFrameAck', { sessionId: event.sessionId }); } catch { /* Closed in finally. */ }
  });
  await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 80, maxWidth: 1440, maxHeight: 1000, everyNthFrame: 1 });
  await input('搜索已加载资源').fill('NO_MATCH_R006'); await visible('当前没有符合条件');
  await button(target === 'web' ? '清除筛选' : '清除筛选并刷新').click();
  await choose(process.env.P11_UI_PHONE); await visible('本地网关未连接');
  assert.equal(await page.locator('textarea,input[placeholder*="Prompt"]').count(), 0);
  await shot(target + '-llm-detail'); record('Concrete phone and independent M token prices; E2E blocked until buyer gateway exists, no prompt collected');
  if (target === 'miniapp') await button('‹ 返回市场').click();
  await choose(process.env.P11_UI_GPU);
  await input('预算上限元').fill('0.01');
  await button(target === 'web' ? '获取正式报价 →' : '获取正式报价').click();
  await visible('预算低于最高预估费用'); assert.equal(quoteRequests, 0, 'Invalid budget must not send a quote write');
  assert.equal(await page.getByText('核对正式合同', { exact: true }).count(), 0, 'Invalid budget cannot create quote');
  await input('预算上限元').fill('5.00');
  await button(target === 'web' ? '获取正式报价 →' : '获取正式报价').click();
  await visible('核对正式合同'); await visible('¥1.80'); await visible('¥0.09');
  const confirmation = button('确认合同并创建订单');
  const isDisabled = () => confirmation.evaluate(el => { const host = el.closest('button,taro-button-core') || el; const value = host.getAttribute('disabled'); return value !== null && value !== 'false' || host.getAttribute('aria-disabled') === 'true'; });
  assert(await isDisabled(), 'Consent cannot default to checked');
  if (target === 'web') await page.locator('input[type="checkbox"]').check();
  else await page.getByText('我确认该个人卖家', { exact: false }).click();
  assert(!(await isDisabled()), 'Explicit consent enables command');
  record('Budget below exact preview sends no quote request; contract consent starts unchecked');
  await confirmation.click(); await visible('结果尚未确认');
  assert.equal(orderRequests.length, 1);
  await shot(target + '-order-pending');
  await page.reload({ waitUntil: 'networkidle' });
  await button('核对原请求').click();
  if (target === 'web') { await visible('订单详情'); receipt.order_id = await page.locator('[data-order-id]').getAttribute('data-order-id'); }
  else { await navigation('订单').click(); await page.locator('.history-item').first().click(); }
  await visible('尚未付款');
  if (target === 'miniapp') receipt.order_id = await page.locator('.order-id').textContent();
  assert.equal(orderRequests.length, 2); assert.deepEqual(orderRequests[0], orderRequests[1]);
  assert(receipt.order_id?.startsWith('ord_'));
  record('Committed order response lost -> reload -> original exact key/body recovered; no duplicate command');
  record('Order remains WAITING_PAYMENT; confirmed usage absent and final fee pending; no client-side billing clock');
  await shot(target + '-order');
  await navigation(target === 'web' ? '我的共享' : '共享').click(); await visible('暂无已确认账单');
  if (target === 'miniapp') await readableButton(button('刷新本人共享状态'));
  await shot(target + '-share'); record('No fabricated seller income or device connection; pairing requires explicit approved-owner confirmation');
  if (providerRound) {
    await input('配对设备名称').fill('合成本机 RTX 4090');
    const confirmPair = async () => target === 'web' ? page.locator('input[aria-label="确认本人设备配对"]').check() : page.getByText('我持有这台设备，将在本机自主确认配对和运行内容。', { exact: true }).click();
    await confirmPair(); await button('创建一次性配对申请').click(); await visible('结果尚未确认');
    await page.reload({ waitUntil: 'networkidle' }); await button('核对原请求').click();
    await navigation(target === 'web' ? '我的共享' : '共享').click();
    if (['R011', 'R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID)) {
      assert(!(await input('配对设备名称').isEnabled()), 'Provider form remains gated during original command recovery');
      releasePairRecovery();
    }
    await visible('原配对申请已确认，凭据不再显示');
    assert.equal(pairRequests.length, 2); assert.deepEqual(pairRequests[0], pairRequests[1]); assert.equal(pairingCredential, '');
    await input('配对设备名称').fill('合成本机 RTX 4090'); await confirmPair();
    if (['R011', 'R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID)) {
      receipt.provider_load_after_recovery = true;
      record('Provider page mounted during held original recovery loads owner profile once idle; no manual reload or automatic write retry');
    }
    const freshPairingResponse = page.waitForResponse(r => r.url().endsWith('/v1/p11/devices/pairing-intents') && r.status() === 201);
    await button('创建一次性配对申请').click();
    pairingCredential = (await (await freshPairingResponse).json()).one_time_token;
    await visible('一次性凭据已遮盖'); assert(typeof pairingCredential === 'string' && pairingCredential.startsWith('p11p.'));
    const stored = await page.evaluate(() => JSON.stringify({ session: { ...sessionStorage }, local: { ...localStorage } }));
    assert(!stored.includes('p11p.') && !stored.includes('one_time_token'), 'Pairing credentials never enter browser storage');
    assert(!(await page.content()).includes(pairingCredential), 'Masked credential does not enter rendered HTML or recording');
    const binary = path.join(root, 'runtime/bin/p11-node.exe'), stateParent = path.join(root, 'runtime/node/provider-validation'); mkdirSync(stateParent, { recursive: true });
    const nativeState = path.join(stateParent, target + '-' + Date.now());
    receipt.provider_native_binary_sha256 = digest(binary); receipt.provider_native_commands = [];
    const native = (action, inputData, expected = 0) => {
      const result = spawnSync(binary, [action, '--state', nativeState], { input: inputData, cwd: root, encoding: 'utf8', windowsHide: true, timeout: 20000 });
      assert.equal(result.status, expected, 'Native control command must enforce its expected result');
      assert(!result.stdout.includes('p11p.') && !result.stderr.includes('p11p.'), 'Native command output must contain no pairing credential');
      const status = JSON.parse(result.stdout); assert.equal(status.execution_enabled, false);
      receipt.provider_native_commands.push({ action, exit_code: result.status, safe_status: status }); return status;
    };
    native('init', JSON.stringify({ control_base: base.origin, model_name: 'RTX 4090 24GB', resource_kind: 'GPU_DEVICE', os_build: 'SYNTHETIC_BROWSER_CONTROL', client_release_id: 'SYNTHETIC_BROWSER_CONTROL' }));
    const enrolled = native('enroll', pairingCredential); pairingCredential = '';
    receipt.provider_device_id = enrolled.device_id; assert(receipt.provider_device_id?.startsWith('dev_'));
    if (['R010','R011','R012','R013','R014','R015','R016','R017','R018','R019'].includes(roundID)) {
      const initialized = native('execution-init'); const reopened = native('execution-status');
      assert.deepEqual(reopened, initialized); assert.equal(initialized.profile, 'P11_NATIVE_EXECUTION_STORE_1');
      assert.equal(initialized.receipt_count, 0); assert.deepEqual(initialized.resources, []);
      receipt.provider_execution_metadata = { initialized_once: true, reopened_same_anchor: true, native_sqlite: true, no_resources_qualified: true, content_received: false };
    }
    native('heartbeat');
    await button('隐藏并清除凭据').click(); await button('刷新本人设备 ↻').click();
    let card = page.locator(`[data-device-id="${receipt.provider_device_id}"]`); await card.waitFor({ state: 'visible' });
    assert((await card.textContent()).includes('已登记，待核验'));
    const cardButton = name => host(card.getByText(name, { exact: true }));
    await cardButton('设置共享条件').click();
    if (target === 'web') await card.locator('input[type="checkbox"]').first().check();
    else await card.getByText('申请按以下条件共享', { exact: true }).click();
    await cardButton('添加共享时段').click();
    const china = ms => new Date(ms + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' ');
    await input('共享开始时间1').fill(china(Date.now() + 5 * 60000)); await input('共享结束时间1').fill(china(Date.now() + 65 * 60000)); await input('预留磁盘MB').fill('1024');
    if (target === 'web') await card.locator('input[aria-label="确认共享条件"]').check();
    else await card.getByText('我确认上述条件；保存后仍等待设备执行与资格核验。', { exact: true }).click();
    await shot(target + '-provider-conditions', card); await cardButton('保存共享条件').click(); await visible('结果尚未确认');
    await page.reload({ waitUntil: 'networkidle' }); await button('核对原请求').click();
    await navigation(target === 'web' ? '我的共享' : '共享').click(); await visible('共享条件已由平台保存');
    card = page.locator(`[data-device-id="${receipt.provider_device_id}"]`); await card.getByText('等待能力核验', { exact: true }).waitFor({ state: 'visible' });
    assert.equal(policyRequests.length, 2); assert.deepEqual(policyRequests[0], policyRequests[1]); assert.equal(policyRequests[0].ifMatch, '"1"');
    const confirmAction = async () => target === 'web' ? card.locator('input[aria-label="确认设备操作责任"]').check() : card.getByText('我已核对型号、设备编号和操作责任。', { exact: true }).click();
    await cardButton('停止接新单').click(); await confirmAction(); await cardButton('确认停止接新单').click(); await visible('已关闭新接单；已有任务仍须实际停止与清理。');
    await cardButton('撤销设备身份').click(); await confirmAction(); await cardButton('确认撤销设备身份').click();
    await card.getByText('身份已撤销', { exact: true }).waitFor({ state: 'visible' });
    native('heartbeat', undefined, 1); receipt.provider_native_processes_exited = true;
    receipt.provider_recovery = { pair_requests: pairRequests.length, original_pair_preserved: JSON.stringify(pairRequests[0]) === JSON.stringify(pairRequests[1]),
      policy_requests: policyRequests.length, original_policy_preserved: JSON.stringify(policyRequests[0]) === JSON.stringify(policyRequests[1]), policy_if_match: policyRequests[0].ifMatch,
      credentials_persisted: false, credential_rendered_or_recorded: false, native_engine_started: false };
    await shot(target + '-provider-revoked', card);
    record('Approved synthetic owner: lost pairing reply reloads the original intent without redisclosing its secret; fresh intent is explicit and credential stays out of storage/HTML/media');
    record('Actual Windows p11-node initializes, enrolls via stdin and reports UNKNOWN; lost policy PATCH recovers exact method/key/body/If-Match once');
    record('Device remains unqualified after policy save; explicit drain/revoke close new admission, and revoked native heartbeat is denied without engine or lock-cleanup claims');
  }
  // Finish the actual transaction recording and close its pages before the
  // narrow-screen journey. A new mobile viewport is set before navigation,
  // rather than resizing an already emulated phone mid-session.
  await cdp.send('Page.stopScreencast'); stoppedAt = performance.now() / 1000;
  await cdp.detach(); cdp = undefined;
  const cookies = await context.cookies();
  await context.close();
  context = await browser.newContext({ viewport: { width: 320, height: 780 }, deviceScaleFactor: 1, locale: 'zh-CN', reducedMotion: 'reduce', ...(target === 'miniapp' ? { isMobile: true, hasTouch: true } : {}) });
  await context.addCookies(cookies);
  await context.route('**/*', route => new URL(route.request().url()).origin === base.origin ? route.continue() : route.abort());
  page = await context.newPage(); page.setDefaultTimeout(12000);
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base.origin + '/' + target + '/', { waitUntil: 'networkidle' });
  await readyMarket();
  await navigation(target === 'web' ? '我的共享' : '共享').click(); await visible('暂无已确认账单');
  await page.waitForFunction(() => innerWidth === 320);
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2), 'No horizontal page overflow at 320px');
  await navigation(target === 'web' ? '我的账号' : '我的').click();
  const logout = button('退出登录');
  await page.waitForFunction(el => { const host = el.closest('button,taro-button-core') || el; const value = host.getAttribute('disabled'); return (value === null || value === 'false') && host.getAttribute('aria-disabled') !== 'true'; }, await logout.elementHandle());
  await logout.click(); await visible(target === 'web' ? '支付宝授权登录 →' : '支付宝授权登录');
  receipt.narrow_viewport = { width: 320, height: 780, fresh_context: true, same_cookie_session: true, navigation_and_logout: true, recorded: false };
  record('Fresh 320px context restores the same cookie session, sharing/account navigation and logout; no horizontal overflow, not native Alipay acceptance');
  assert.deepEqual(errors, []);
  receipt.passed = true;
} catch (error) {
  receipt.failure = String(error?.stack || error);
  if (page) { try { await page.screenshot({ path: path.join(runtime, 'failure.png'), fullPage: true }); writeFileSync(path.join(runtime, 'failure.html'), await page.content()); } catch { /* Browser may have exited. */ } }
} finally {
  releasePairRecovery();
  if (cdp) { try { await cdp.send('Page.stopScreencast'); stoppedAt = performance.now() / 1000; await cdp.detach(); } catch { /* Browser close remains mandatory. */ } }
  if (context) await context.close();
  if (browser) { await browser.close(); receipt.browser_closed = !browser.isConnected(); receipt.remaining_owned_tabs = 0; }
}
if (receipt.passed && frames.length >= 5 && stoppedAt) {
  const concat = [];
  for (let i = 0; i < frames.length; i++) concat.push(`file '${frames[i].filename}'`, `duration ${Math.max(.001, (frames[i + 1]?.timestamp || stoppedAt) - frames[i].timestamp).toFixed(6)}`);
  concat.push(`file '${frames.at(-1).filename}'`); writeFileSync(path.join(runtime, 'concat.txt'), concat.join('\n') + '\n');
  const mp4 = path.join(output, 'recordings', target + '-transaction.mp4'), gif = path.join(output, 'recordings', target + '-transaction.gif');
  const ffmpeg = process.env.P11_FFMPEG_PATH || 'ffmpeg';
  const width = target === 'web' ? 1440 : 390, height = target === 'web' ? 1000 : 844;
  const normalize = `fps=12,scale=${width}:${height}:force_original_aspect_ratio=decrease,pad=${width}:${height}:(ow-iw)/2:(oh-ih)/2:color=0xfff5c5,setsar=1`;
  for (const args of [ ['-f','concat','-safe','0','-i','concat.txt','-vf',normalize,'-c:v','libx264','-preset','fast','-crf','25','-pix_fmt','yuv420p','-movflags','+faststart',mp4],
    ['-i',mp4,'-filter_complex','fps=5,scale=600:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=96[p];[b][p]paletteuse=dither=bayer','-loop','0',gif] ]) {
    const r = spawnSync(ffmpeg, ['-hide_banner','-loglevel','error','-y',...args], { cwd: runtime, encoding: 'utf8', windowsHide: true, timeout: 90000 });
    if (r.status !== 0) { receipt.passed = false; receipt.failure = 'Actual browser recording encode failed: ' + r.stderr; break; }
  }
  if (receipt.passed) receipt.recording = { method: 'Native Chromium CDP compositor screencast, real timestamps; silent', frames: frames.length, seconds: +(stoppedAt - frames[0].timestamp).toFixed(3), mp4: { path: path.relative(root,mp4).replaceAll('\\','/'), sha256: digest(mp4) }, gif: { path: path.relative(root,gif).replaceAll('\\','/'), sha256: digest(gif) } };
}
const buildReceipts = ['check','test','build'].map(name => path.join(root, 'runtime/frontend', name + '-receipt.json')).filter(existsSync);
receipt.build_receipt_sha256 = Object.fromEntries(buildReceipts.map(f => [path.relative(root,f).replaceAll('\\','/'),digest(f)]));
writeFileSync(path.join(runtime, 'receipt.json'), JSON.stringify(receipt,null,2)+'\n');
if (receipt.passed) writeFileSync(path.join(root, 'dev/tests/evidence/' + roundID + '-' + target + '-browser.json'), JSON.stringify(receipt,null,2)+'\n');
process.stdout.write(JSON.stringify(receipt) + '\n');
if (!receipt.passed) process.exitCode = 1;
