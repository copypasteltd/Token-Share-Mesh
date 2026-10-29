import { chromium } from 'playwright-core';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { createHash } from 'node:crypto';
import { createInterface } from 'node:readline';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
const output = path.join(root, 'dev/media');
const capture = path.join(root, 'runtime/capture/readers-' + Date.now());
const framesDir = path.join(capture, 'frames');
for (const dir of [framesDir, path.join(output, 'screenshots'), path.join(output, 'recordings')]) mkdirSync(dir, { recursive: true });
process.env.TEMP = process.env.TMP = path.join(root, 'runtime/tmp/readme-browser');
mkdirSync(process.env.TEMP, { recursive: true });
const browserPath = process.env.P11_BROWSER_PATH || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
].find(existsSync);
if (!browserPath || !existsSync(browserPath)) throw new Error('Set P11_BROWSER_PATH to an existing native Chromium/Edge/Chrome executable');
const ffmpeg = process.env.P11_FFMPEG_PATH || 'ffmpeg';
const ffprobe = process.env.P11_FFPROBE_PATH || 'ffprobe';
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const rel = name => name.replaceAll(path.sep, '/');
const digest = name => createHash('sha256').update(readFileSync(name)).digest('hex');
function encode(args) {
  const result = spawnSync(ffmpeg, ['-hide_banner', '-loglevel', 'error', '-y', ...args], { cwd: capture, encoding: 'utf8', windowsHide: true, timeout: 120000 });
  if (result.status !== 0) throw new Error('FFmpeg failed: ' + (result.stderr || result.error?.message || result.status));
}

let browser, preview, previewStopped = false, browserStopped = false;
let version = '', recordingSeconds = 0;
const errors = [], assertions = [], timeline = [], frames = [];
const mark = action => timeline.push({ seconds: Number((performance.now() / 1000).toFixed(3)), action });
try {
  preview = spawn(process.env.P11_PYTHON || 'python', ['-X', 'utf8', path.join(root, 'dev/tools/预览文档.py'), '--port', '0'], { cwd: root, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
  const lines = createInterface({ input: preview.stdout });
  const address = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Local reader preview did not start')), 10000);
    lines.once('line', line => { clearTimeout(timer); try { resolve(JSON.parse(line)); } catch (error) { reject(error); } });
    preview.once('error', error => { clearTimeout(timer); reject(error); });
    preview.once('exit', code => { clearTimeout(timer); reject(new Error('Preview exited before capture: ' + code)); });
  });
  browser = await chromium.launch({ executablePath: browserPath, headless: true, args: ['--disable-extensions', '--no-first-run', '--disable-background-networking'] });
  version = browser.version();
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, locale: 'zh-CN', reducedMotion: 'reduce' });
  await context.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort());
  const page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(address.whitepaper, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  if (!(await page.title()).includes('白皮书')) throw new Error('Wrong whitepaper page');
  await page.screenshot({ path: path.join(output, 'screenshots/whitepaper-desktop.png') });
  assertions.push('Whitepaper reader loaded at 1440x900');
  const session = await context.newCDPSession(page);
  session.on('Page.screencastFrame', async event => {
    const filename = `frame-${String(frames.length).padStart(6, '0')}.jpg`;
    const timestamp = performance.now() / 1000;
    frames.push({ filename, timestamp });
    writeFileSync(path.join(framesDir, filename), Buffer.from(event.data, 'base64'));
    await session.send('Page.screencastFrameAck', { sessionId: event.sessionId });
  });
  await session.send('Page.startScreencast', { format: 'jpeg', quality: 82, maxWidth: 1440, maxHeight: 900, everyNthFrame: 1 });
  mark('whitepaper overview');
  await pause(1200);
  await page.locator('#search').click();
  await page.locator('#search').pressSequentially('端对端', { delay: 180 });
  await page.waitForFunction(() => document.querySelectorAll('#results a').length > 0);
  const matches = await page.locator('#results a').count();
  assertions.push(`Whitepaper full-text search produced ${matches} visible results`);
  mark('search for end-to-end encryption');
  await pause(1500);
  await page.screenshot({ path: path.join(output, 'screenshots/whitepaper-search.png') });
  await page.locator('#results a').first().click();
  await pause(1000);
  if (!new URL(page.url()).hash) throw new Error('Search result navigation did not reach a chapter');
  assertions.push('Search result link navigated to a real chapter anchor');
  mark('open a search result');
  for (let i = 0; i < 18; i++) { await page.mouse.wheel(0, 44); await pause(80); }
  await pause(1100);
  await page.locator('#search').fill('NO_MATCH_TOKEN_SHARE_MESH_314159');
  await page.waitForFunction(() => document.querySelector('#search-status').textContent.includes('0'));
  assertions.push('No-match search has an explicit zero-result state');
  await page.locator('#search').press('Escape');
  await page.waitForFunction(() => document.querySelector('#search-panel').style.display === 'none');
  assertions.push('Escape clears search and hides result panel');
  await page.goto(address.technical, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  if (!(await page.title()).includes('技术')) throw new Error('Wrong technical page');
  await page.screenshot({ path: path.join(output, 'screenshots/technical-desktop.png') });
  mark('technical implementation reader');
  await pause(1600);
  for (let i = 0; i < 26; i++) { await page.mouse.wheel(0, 38); await pause(80); }
  await pause(1300);
  await session.send('Page.stopScreencast');
  const stoppedAt = performance.now() / 1000;
  await session.detach();
  if (frames.length < 15) throw new Error('Insufficient actual compositor frames for a recording');
  recordingSeconds = stoppedAt - frames[0].timestamp;
  const concat = [];
  for (let i = 0; i < frames.length; i++) {
    const duration = Math.max(0.001, (frames[i + 1]?.timestamp || stoppedAt) - frames[i].timestamp);
    concat.push(`file 'frames/${frames[i].filename}'`, `duration ${duration.toFixed(6)}`);
  }
  concat.push(`file 'frames/${frames.at(-1).filename}'`);
  writeFileSync(path.join(capture, 'concat.txt'), concat.join('\n') + '\n');
  await context.close();
  assertions.push('Desktop capture context closed');

  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1, isMobile: true, hasTouch: true, locale: 'zh-CN' });
  await mobile.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort());
  const mobilePage = await mobile.newPage();
  await mobilePage.goto(address.whitepaper, { waitUntil: 'networkidle' });
  await mobilePage.screenshot({ path: path.join(output, 'screenshots/whitepaper-mobile.png') });
  const mobileWidth = await mobilePage.evaluate(() => ({ viewport: innerWidth, content: document.documentElement.scrollWidth }));
  assertions.push(`Mobile-width document: viewport=${mobileWidth.viewport}, content=${mobileWidth.content}`);
  if (mobileWidth.content > mobileWidth.viewport + 2) throw new Error('Mobile reader has unexpected horizontal overflow');
  await mobile.close();
  if (errors.length) throw new Error('Browser page errors: ' + errors.join('; '));
} finally {
  if (browser) { await browser.close(); browserStopped = !browser.isConnected(); }
  if (preview && preview.exitCode === null) {
    const exit = once(preview, 'exit');
    preview.kill();
    let timer;
    try { await Promise.race([exit, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('Owned preview process stop not confirmed')), 8000); })]); }
    finally { clearTimeout(timer); }
  }
  previewStopped = !preview || preview.exitCode !== null || preview.signalCode !== null;
}
if (!browserStopped || !previewStopped) throw new Error('Owned browser or preview cleanup not confirmed');
console.log('Owned browser contexts and local preview closed');
const video = path.join(output, 'recordings/readers-tour.mp4');
const gif = path.join(output, 'recordings/readers-tour.gif');
encode(['-f', 'concat', '-safe', '1', '-i', 'concat.txt', '-vf', 'fps=24,format=yuv420p', '-an', '-c:v', 'libx264', '-preset', 'medium', '-crf', '23', '-movflags', '+faststart', video]);
encode(['-i', video, '-filter_complex', 'setpts=PTS/1.5,fps=8,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=192[p];[s1][p]paletteuse=dither=bayer', '-loop', '0', gif]);
const probe = spawnSync(ffprobe, ['-v', 'error', '-show_format', '-show_streams', '-of', 'json', video], { encoding: 'utf8', windowsHide: true });
if (probe.status !== 0) throw new Error('MP4 probe failed');
encode(['-v', 'error', '-i', video, '-f', 'null', '-']);
const probeData = JSON.parse(probe.stdout);
const filenames = ['architecture.svg', 'screenshots/whitepaper-desktop.png', 'screenshots/whitepaper-search.png', 'screenshots/technical-desktop.png', 'screenshots/whitepaper-mobile.png', 'recordings/readers-tour.mp4', 'recordings/readers-tour.gif'];
const manifest = {
  round: JSON.parse(readFileSync(path.join(root, 'dev/progress/implementation-status.json'), 'utf8')).round, captured_at: new Date().toISOString(), scope: 'Real repository documentation readers only; no trading, inference, payment or physical-device demonstration',
  browser: { engine: 'Native Chromium', version, viewport: { width: 1440, height: 900 }, mobile_viewport: { width: 390, height: 844 }, closed: browserStopped, remaining_owned_tabs: 0 },
  preview_process_stopped: previewStopped, virtualization_used: false,
  recording: { method: 'CDP Page.startScreencast: actual browser compositor frames and received timestamps', actual_frames: frames.length, duration_seconds: Number(recordingSeconds.toFixed(3)), gif_playback_speed: 1.5, audio: 'none', video: probeData },
  sources: ['docs/白皮书/算力共享平台完整阅读版-v3.0.html', 'docs/技术开发/技术开发文档完整阅读版-v1.0.html'].map(name => ({ path: name, sha256: digest(path.join(root, name)) })),
  timeline: timeline.map(entry => ({ ...entry, seconds: Number((entry.seconds - frames[0].timestamp).toFixed(3)) })),
  assertions, page_errors: errors,
  assets: filenames.map(name => ({ path: 'dev/media/' + name, kind: name.endsWith('.svg') ? 'AUTHORED_DESIGN_DIAGRAM' : name.endsWith('.png') ? 'ACTUAL_BROWSER_SCREENSHOT' : 'ACTUAL_BROWSER_RECORDING', sha256: digest(path.join(output, name)), bytes: readFileSync(path.join(output, name)).length })),
};
writeFileSync(path.join(output, 'media-manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(JSON.stringify({ status: 'CAPTURED_AND_DECODED', actual_frames: frames.length, seconds: Number(recordingSeconds.toFixed(2)), files: filenames.length, browser_closed: browserStopped, preview_stopped: previewStopped }));
