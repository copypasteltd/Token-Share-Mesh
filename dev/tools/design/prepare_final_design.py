"""Prepare final neubrutalist image briefs; no product code or engine execution."""
from __future__ import annotations
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[3]
DOC=ROOT/'dev/design/新粗野主义-v1.0'
OUT=ROOT/'output/imagegen/2026-10-30-04-最终设计约束'
OLD=ROOT/'output/imagegen/2026-10-30-五种结构风格-V2/04-硬边交易所-新粗野主义/01-小程序-首页.png'

COMMON='''Create ONE final high-fidelity Chinese UI constraint image for “算力共享” / “Token Share Mesh”, a C2C marketplace of personal idle GPUs and phones. The user has chosen direction 04 NEUBRUTALISM. Preserve that family from the attached reference: pure black heavy Chinese headings, 3px logical black outlines, sharp corners, hard black offset shadows with ZERO blur, warm pale-yellow canvas, white ticket-like panels, mustard active cells, orange primary actions, a chunky three-connected-node mesh brand mark. Distinct strong compositional hierarchy, disciplined spacing, functional trade tickets, not generic rounded SaaS cards. NO soft shadows, NO glass, NO pastels, NO gradients on UI fills; isolated device illustrations may have natural shading.
The image is one flat straight-on edge-to-edge screen. No mockup bezel, no phone floating on desk, no perspective, no multi-screen collage, no design labels outside UI, no watermarks. High readability. Correct concise Simplified Chinese. This is a demo concept, not evidence of implemented payments/devices.
CRITICAL NEW BILLING RULE OVERRIDES OLD REFERENCE: NEVER use 张, 千Token, 千token, /次, or /分钟 as a billing price unit. GPU rental is “¥3.60 / 小时”. LLM input is “输入 ¥0.80 / M token”, output “输出 ¥1.60 / M token”. Write exactly “M token” with space; it means one million tokens, not per thousand. Duration labels like 30分钟 are allowed as time controls, NEVER per-minute price. Remove all old reference price/unit text.
GPU seller is 星河同学 / 个人卖家; resource RTX 4090 · 24GB; capability ComfyUI · 图像生成; mode 显卡租赁; privacy 标准隐私. GPU rental is not automatically E2E: NEVER place 平台不可见 on this rental offer. A separate personal phone offer is 摩托罗拉 X30, seller 阿澄 / 个人卖家, model Qwen 3B · 对话, mode 端对端加密, privacy 平台不可见；执行设备仍需处理明文. Never label any device merely Android设备 or 手机推理. The separate provider's own phone draft is 努比亚 Z20, owned by 星河同学; don't confuse it with 阿澄’s phone.
Rates and device availability are example data. Do not invent ratings, sales, guaranteed profits, hardware-confidential badges, balances, withdrawal success or paid-success claims. Same glyph, font character, corner rules, outlines, hard shadow, icon line weight and active-state treatment across every page.
'''

PAGES=[
 dict(id='UI-P01',name='小程序-算力市场',size='1024x1536',kind='anchor',brief='''TARGET: ONE Alipay MINI-PROGRAM MARKET HOME, 1024x1536 portrait. Use the selected reference's bold trade-ticket visual system and basic home composition, but update the data/units and mode semantics.
Top mobile status and proper Alipay capsule, bold 算力共享 brand, outlined search 搜索型号、模型或卖家. Three square shortcut controls 明文 / 端对端加密 / 显卡租赁 are CATEGORY ENTRY points, NO privacy mode is preselected across this mixed market. Four small category cells 全部 / 对话 / 图像 / 视频, 全部 selected mustard.
Main large GPU ticket: illustrated GPU and RTX 4090 · 24GB at left, dominant orange price block “¥3.60 / 小时” at right. Lower ticket: 星河同学, 个人卖家, ComfyUI · 图像生成, explicit 显卡租赁 and 标准隐私 labels, 查看服务 hard-shadow action. Do NOT mark this GPU rental 平台不可见.
Second compact phone ticket: actual name 摩托罗拉 X30, Qwen 3B, 阿澄 / 个人卖家. It has two crisp smaller price rows “输入 ¥0.80 / M token” and “输出 ¥1.60 / M token”, with 端对端加密 badge and short 平台不可见 label. Avoid crowding. The phone's concrete model must be prominent.
Bottom four square nav cells 发现 / 订单 / 共享 / 我的, 发现 selected. Fit both full resource tickets above the bottom nav. No gateway or fees panels on this discovery page.'''),
 dict(id='UI-P02',name='小程序-LLM服务详情',size='1024x1536',kind='page',brief='''TARGET: ONE Alipay MINI-PROGRAM LLM SERVICE DETAIL for the reference's PHONE offer, portrait 1024x1536. Same top status, Alipay capsule, heavy type and ticket grammar; back arrow and 服务详情. This page is NOT the GPU detail.
Hero trade ticket: modest Motorola phone illustration, large 摩托罗拉 X30, capability Qwen 3B · 对话, seller 阿澄 / 个人卖家. A bold rate area clearly separates “输入 ¥0.80 / M token” and “输出 ¥1.60 / M token”. Do not show the GPU's hourly price anywhere on this phone page.
Feature cells say 本地推理 / OpenAI兼容 / Anthropic兼容. Privacy mode is explicitly 端对端加密 with selected yellow/orange square cell and readable note “平台不可见；执行设备仍需处理明文”. No claim of absolute secrecy from hardware owner.
Contract controls below: “预算上限 ¥5.00”, explanatory short text “输入、输出分别计量” and “最终按实际用量结算”. Platform fee is already included; do not add another buyer fee or fabricate exact final token cost before usage. Connection row says “本地网关未连接”.
Fixed bottom action area has a small “尚未产生费用” label and big orange hard-shadow “连接网关并继续” button. No bottom tabs. The user confirms the seller, rates, privacy and budget before any future paid call. No payment-success badge, no plaintext prompt field.'''),
 dict(id='UI-P03',name='小程序-GPU订单详情',size='1024x1536',kind='page',brief='''TARGET: ONE Alipay MINI-PROGRAM GPU RENTAL ORDER DETAIL, portrait 1024x1536, same chosen style. Top back arrow 订单详情, mobile status and Alipay capsule. A strong outlined status banner says 执行中 and small order id TSM20261030001.
Resource ticket: RTX 4090 · 24GB, seller 星河同学 / 个人卖家, ComfyUI · 图像生成, 显卡租赁 / 标准隐私. Price is ¥3.60 / 小时. This is standard-privacy controlled GPU rental; do NOT show 平台不可见 or 端对端加密 as the selected rental mode.
Main two-column clock/fee panel: “已计费时长 10分钟” on left and “当前费用 ¥0.60” on right, very readable strong numerals. Below use a simple bounded progress bar, no chart: 本次时长上限 30分钟, 剩余 20分钟. Another concise row shows “预算上限 ¥5.00” and “30分钟预计 ¥1.80”. Show a small “当前费用待最终核对” helper, not a settled bill.
Simple four-stage strip 就绪 / 执行中 / 停止清理 / 已结束, 执行中 active. Contract summary row and 查看合同 control. Use a small output/usage area saying 在工作台使用算力, no plaintext prompt, no generated-image thumbnail or secrets.
Fixed bottom has a secondary 联系卖家 control and a strong orange 结束会话 action. An active session can request stop; don't imply it is already stopped or funds refunded. No discovery tab bar and no fake payout.'''),
 dict(id='UI-P04',name='小程序-提供方共享',size='1024x1536',kind='page',brief='''TARGET: ONE Alipay MINI-PROGRAM PROVIDER SHARING DASHBOARD for current user 星河同学, portrait 1024x1536, same chosen style. Small status/capsule, title 我的共享 and small user 星河同学. This is C2C personal supply, not platform-owned inventory.
Compact outlined summary strip: “运行设备 1”, “预估服务收入 ¥0.57”, and small “未结算” note. Do not show a withdrawable wallet or guarantee earnings.
First large device ticket is current user's RTX 4090 · 24GB, status 运行中, capability ComfyUI, mode 显卡租赁 / 标准隐私, rate ¥3.60 / 小时. Small state text 当前会话 10分钟. Controls 查看会话 and 停止接新单. Stopping new orders must not claim instantly killing the active task.
Second smaller device ticket is the SAME user's separate own phone “努比亚 Z20”, Qwen 3B, status 未上架. Show draft rates “输入 ¥0.80 / M token” and “输出 ¥1.60 / M token”, with 草稿报价 caption and 编辑挂牌 action. This device is NOT 阿澄’s 摩托罗拉 X30; do not label it 阿澄.
An outlined 添加设备 button and short helper “运行设备客户端后配对”. A compact preference cell may say 仅充电时共享 for the phone draft, without implying it is already active. All device models explicit, no generic Android device label.
Bottom square nav 发现 / 订单 / 共享 / 我的 with 共享 selected. Keep the interface compact and readable with hard ticket borders and flat yellow/orange accents.'''),
 dict(id='UI-P05',name='Web-算力工作台',size='1536x1024',kind='page',brief='''TARGET: ONE desktop WEB buyer compute-market workspace, landscape 1536x1024. Use the reference's chosen NEUBRUTALIST style, translated into a genuine desktop composition. NO mobile status bar, NO Alipay capsule, NO phone-screen wrapper on desktop.
Strong horizontal top navigation: brand 算力共享, 工作台 / 算力市场 / 我的订单 / 本地网关 / API密钥 / 账单, 算力市场 selected. Small ordinary account avatar at far right. Heading 算力工作台 plus outlined search.
Three square mode shortcut entries 明文 / 端对端加密 / 显卡租赁 and a slim category row 全部 / 对话 / 图像 / 视频, 全部 active; these shortcuts do not falsely mark all mixed offers as E2E.
Left two-thirds is a wide chunky outlined resource comparison table with columns 设备与用途 / 个人卖家 / 模式 / 单价. GPU selected row: RTX 4090 · 24GB, ComfyUI · 图像生成, 星河同学, 显卡租赁, 标准隐私, ¥3.60 / 小时. Second row: 摩托罗拉 X30, Qwen 3B · 对话, 阿澄, 端对端加密, 平台不可见, two price lines 输入 ¥0.80 / M token and 输出 ¥1.60 / M token. Each resource stays a different seller/contract.
Right third is a yellow hard-shadow selected-GPU contract ticket: 星河同学, RTX 4090 · 24GB, 显卡租赁 / 标准隐私, rate ¥3.60 / 小时, “使用时长 30分钟”, “预算上限 ¥5.00”. Exact estimated price rows: 卖家服务 ¥1.71, 平台服务费 ¥0.09, 合计 ¥1.80. Fees are included, no added charge. Readable short state “等待就绪后起计时”. Primary orange action “确认租赁条件”, secondary “查看合同”. No payment-success or fake reserve/freeze state, and no E2E claim for the selected standard rental.
Keep visible hard black border, hard offset shadow, sharp geometry and bold type from the selected family. No dashboard charts, no KPI wall, no soft/glass cards. Full page in one frame.'''),
]

def main():
    OUT.mkdir(parents=True,exist_ok=True);(OUT/'prompts').mkdir(exist_ok=True);(OUT/'production').mkdir(exist_ok=True)
    seed=json.loads((DOC/'示例数据.json').read_text(encoding='utf-8'))
    jobs=[]
    anchor=OUT/'UI-P01-小程序-算力市场.png'
    for p in PAGES:
        path=OUT/'prompts'/(p['id']+'.txt')
        prompt=COMMON+'\n'+('REFERENCE: selected old direction 04; preserve its visual grammar, replace old units and old privacy-mode assumptions.\n' if p['kind']=='anchor' else 'REFERENCE: the final market page is the family anchor. Match its actual brand, font, palette, hard borders/shadows and ticket components, while drawing ONLY the NEW target page below. Never paste the reference page inside the result.\n')+'\n'+p['brief']
        path.write_text(prompt,encoding='utf-8')
        jobs.append(dict(**p,model='gpt-image-2.5',quality='high',output=str((OUT/(p['id']+'-'+p['name']+'.png')).relative_to(ROOT)),prompt=str(path.relative_to(ROOT)),reference=str((OLD if p['kind']=='anchor' else anchor).relative_to(ROOT))))
    (OUT/'production/jobs.json').write_text(json.dumps(dict(design_version='NB-1.0',source='dev/design/新粗野主义-v1.0/示例数据.json',jobs=jobs),ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    (OUT/'production/demo-data.json').write_text(json.dumps(seed,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print('Prepared 5 final constraint images: four mini-program pages and one desktop page; billing units hours / M token.')

if __name__=='__main__':main()
