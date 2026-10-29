"""Prepare scoped image-generation prompts and style tokens; no platform development."""
from __future__ import annotations
import json
from pathlib import Path

BASE = Path(__file__).resolve().parents[1]
STYLES = [
    {'id':'01','name':'晴空秩序','slug':'01-晴空秩序','palette':{'background':'#F5F8FF','surface':'#FFFFFF','primary':'#2463EB','text':'#12223B','border':'#DEE7F2','secondary':'#6C8BB6'},'direction':'A highly polished native Alipay mini-program design, calm sky-blue clarity and financial-grade trust without looking like a bank. Crisp 12–16 logical px rounded cards, balanced density, strong alignment, modern Chinese sans serif with medium headings. White cards on a very light ice-blue canvas; pure solid blue primary buttons. Signature: a precise three-node blue mesh mark and one understated connection line. Compact, useful, legible. No gradients as decoration, no cartoon, no glassmorphism.'},
    {'id':'02','name':'薄荷织网','slug':'02-薄荷织网','palette':{'background':'#F2F8F5','surface':'#FFFFFF','primary':'#29896C','text':'#1B4B3B','border':'#D4E9DE','secondary':'#A6DCC4'},'direction':'A humane community-compute product with a cool mint and deep forest visual identity. Airy spacing, 24–28 logical px softly rounded cards and pill controls, gentle layered surfaces, humanist Chinese sans serif, generous touch targets. Signature: a very subtle thin connecting thread relating provider avatars and resource cards, suggesting voluntary peer sharing. Mint is structural, not botanical illustration. White and green, not generic beige; no stock photos, no cartoon leaves, no heavy blur.'},
    {'id':'03','name':'钛银精密','slug':'03-钛银精密','palette':{'background':'#EEF0F2','surface':'#FFFFFF','primary':'#6673DC','text':'#262B36','border':'#CDD2DA','secondary':'#89939F'},'direction':'Premium titanium-silver precision, practical instrument-panel clarity translated into consumer software. Light silver neutral canvas, white or very pale metallic panels, restrained periwinkle-blue primary actions, graphite Chinese sans serif, monospaced numeric specs, 10–14 logical px corners. Signature: carefully aligned horizontal device-spec rails and small engraved-looking specification capsules. Precise hairline dividers and subtle tonal depth, not glossy chrome. No sci-fi cockpit, no dark theme, no decorative dashboards or fake analytics.'},
    {'id':'04','name':'紫夜星轨','slug':'04-紫夜星轨','palette':{'background':'#17172B','surface':'#24243D','primary':'#BFA5FF','text':'#F3F2FF','border':'#3D3C5B','secondary':'#A6C5FF'},'direction':'Quiet violet night: a refined deep-indigo and plum productivity interface with pale lavender actions and cool ice-blue secondary details. Dark purple rather than black. 18–22 logical px corners, restrained layered cards, exceptionally legible off-white Chinese type, soft clear borders. Signature: a faint orbital line in the top header and short connection arcs in device cards, suggesting a distributed mesh. No acid green, no cyberpunk, no RGB gamer neon, no bright glow cloud and no noisy star field.'},
    {'id':'05','name':'珊瑚拼图','slug':'05-珊瑚拼图','palette':{'background':'#FFF9F7','surface':'#FFFFFF','primary':'#E87558','text':'#263A48','border':'#F1D5CB','secondary':'#65A8AB'},'direction':'Friendly coral modularity: a fresh consumer technology marketplace, warm almost-white canvas, solid coral primary actions, muted blue-teal accents and dark blue-grey typography. Confident bold Chinese sans-serif headings paired with simple readable body type, 16–20 logical px cards and a flat matte finish. Signature: two interlocking coral/teal geometric modules forming the mesh brand mark, repeated discreetly as capability badges. Structured warm modernity, not beige serif editorial, not a children’s cartoon, no stickers, no multicolor rainbow.'},
]

COMMON = '''Create a production-quality HIGH-FIDELITY UI CONCEPT DESIGN for Token Share Mesh, a C2C compute marketplace in Chinese. This is a design study, not a live product screenshot.
The actual sellers are individual people voluntarily listing their idle GPUs and Android phones. Never portray platform-owned compute inventory.
Use exactly one flat, straight-on, edge-to-edge app page per image. No device mockup, no phone bezel, no perspective, no hand, no desk, no second screen, no collage, no color-swatch board, no annotations, no watermark. The requested image IS the interface, with disciplined consistent real components. Draw the UI itself, not a presentation slide.
Brand title: “算力共享”. Optional small English brand: “Token Share Mesh”. Use a minimal three-connected-node mesh glyph, preserving it throughout this style family.
Exact consistent demonstration data: primary listing “RTX 4090 · 24GB”; seller “星河同学”; capability “ComfyUI · 图像生成”; price “¥0.60 / 张”. A secondary Android listing may show “手机推理 · Qwen 3B”, seller “阿澄”, “¥0.02 / 千Token”. Do not invent ratings, huge sales counts, guaranteed earnings, or paid-success badges.
The three service modes are “明文”, “端对端加密”, “显卡租赁”. Highlight “端对端加密”. Within that selected privacy level the platform does not see content, but the execution device processes plaintext. Never claim absolute invisibility to the device owner or verified confidential hardware.
Use concise correct Simplified Chinese, large enough to read, with generous line height and sane spacing. Labels should be realistic and consistent rather than gibberish. Hardware names and currency must be especially crisp. Content should fit in the screen and align to a coherent grid.
Treat typography, corner radii, icon stroke width, surfaces, shadows, provider avatars, mode chips, active states, button geometry and accent colors as one frozen design system. Use the stated palette roles, do not print hex codes in the interface.
'''

PAGES = {
    'home': '''TARGET: ONE Alipay mini-program BUYER MARKET HOME, portrait 1024x1536. A neat mobile status bar at top and the proper small Alipay-style capsule control in the top-right; not a full web browser toolbar.
Layout from top to bottom: compact brand/navigation header; confident short heading “发现合适的算力”; search field “搜索模型、设备或卖家”; three-mode segmented switch with “端对端加密” active; four small category chips “全部 / 对话 / 图像 / 视频”, “全部” active; a section title “个人卖家精选”; one prominent main GPU listing card and one smaller Android resource card. The GPU card shows the exact primary seller, device, capability, “平台不可见” badge, and ¥0.60 / 张 price. Show the seller avatar as a small, tasteful styled avatar, not a company logo. Include a clear short “查看服务” action on the main card. Let resource thumbnails be clean symbolic GPU/mobile illustrations, not random photography.
Bottom fixed mini-program navigation has four items with matching line icons: “发现”, “订单”, “共享”, “我的”; “发现” active. Preserve a proper bottom safe area. The main visual emphasis should be the primary device/seller card and a clear privacy selection, not a giant decorative marketing banner. Two complete readable resource cards are enough; do not overcrowd.''',
    'detail': '''TARGET: ONE Alipay mini-program SERVICE DETAIL page, portrait 1024x1536, for the SAME GPU listing on the reference home. It belongs to exactly the same product and theme as the reference image.
Reuse the identical top status bar and Alipay capsule, brand glyph, heading weights, corner radii, icons, colors, spacing and card treatment from the reference. The title row has a back arrow and “服务详情”.
Below it show a restrained GPU visual and “RTX 4090 · 24GB”, then “ComfyUI · 图像生成”. Provider identity row: same avatar style, “星河同学”, “个人卖家”. Show short spec chips “SDXL 1.0”, “24GB显存”, “按张计费”.
A clean privacy section has the same three mode choices, with “端对端加密” selected and the concise note “平台不可见；执行设备仍需处理明文”. This is meaningful privacy information, not dense technical documentation.
A neat price summary shows exact values “卖家服务 ¥0.57”, “平台服务费 ¥0.03”, “合计 ¥0.60”. Show budget control “预算上限 ¥5.00”. A small connection state says “本地网关未连接”.
At bottom use a fixed action bar, styled exactly like the reference primary buttons, with total “¥0.60 / 张” and the primary action “连接网关并继续”. Use no fake paid-success badge and no claim of hardware-attested secrecy. No bottom tab navigation on this detail page; its fixed price/action bar uses the same safe-area treatment as the home. Keep the entire page comprehensible and balanced.''',
    'web': '''TARGET: ONE DESKTOP WEB COMPUTE WORKSPACE page, landscape 1536x1024, the responsive desktop companion of the reference mini-program, not an enlarged phone page.
The reference is the visual-system anchor: preserve the same palette roles, glyph, primary action geometry, card corners, icon stroke, typographic character, provider/avatar treatment and privacy badges. Translate the component system into a polished desktop layout.
Use a quiet left navigation rail about 200 logical px wide with the brand and entries “工作台”, “算力市场”, “我的订单”, “本地网关”, “API密钥”, “账单”. “算力市场” is selected. Header says “算力工作台” and has a restrained search and account avatar.
The main content heading is “选择算力，连接你的应用”. Present the same three mode tabs with “端对端加密” selected and the same four category chips. Use two primary resource cards in a readable desktop grid, including the exact “RTX 4090 · 24GB”, seller “星河同学”, capability “ComfyUI · 图像生成”, price “¥0.60 / 张”, and the Android listing if space allows. The selected GPU card uses this family's active-border treatment.
A right-side service/contract summary panel shows the same selected seller and resource, “端对端加密”, “平台不可见；执行设备仍需处理明文”, “预算上限 ¥5.00”, the fee breakdown ¥0.57 + ¥0.03 = ¥0.60, and “本地网关未连接”. The primary action is “连接网关”, secondary “查看合同”. Preserve a clear reading hierarchy and useful whitespace.
No income charts, no fake analytics, no meaningless KPI wall, no source code block, no phone floating inside the web page, no platform-owned GPU fleet. This is a useful C2C buyer workspace with contract and gateway awareness.''' }


def main():
    jobs=[]
    for style in STYLES:
        folder=BASE/style['slug']
        prompt_dir=folder/'prompts'
        prompt_dir.mkdir(parents=True,exist_ok=True)
        (folder/'style-tokens.json').write_text(json.dumps(style,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        for page,filename,size in [('home','01-小程序-首页.png','1024x1536'),('detail','02-小程序-服务详情.png','1024x1536'),('web','03-Web-算力工作台.png','1536x1024')]:
            text=COMMON+'\nSTYLE FAMILY '+style['id']+' — '+style['name']+'\n'+style['direction']+'\nExact palette roles: '+json.dumps(style['palette'])+'\n\n'
            if page!='home':
                text+='REFERENCE CONSISTENCY RULE: The attached image is this exact style family’s approved home-screen anchor. Generate the NEW target page below using that visual system. Do not copy the old page content. Do not put the reference image as a thumbnail inside the result. No blending with another style family. Match actual observed component design as closely as the new target layout permits.\n\n'
            text+=PAGES[page]+'\n\nPolish the final result until it looks like a carefully art-directed real Chinese app design, with no presentation extras.'
            prompt=prompt_dir/(page+'.txt')
            prompt.write_text(text,encoding='utf-8')
            jobs.append({'id':style['id']+'-'+page,'style':style['id'],'style_name':style['name'],'page':page,'model':'gpt-image-2.5','quality':'high','size':size,'output':str((folder/filename).relative_to(BASE)),'prompt':str(prompt.relative_to(BASE)),'reference':None if page=='home' else str((folder/'01-小程序-首页.png').relative_to(BASE))})
    (BASE/'production/jobs.json').write_text(json.dumps({'model':'gpt-image-2.5','jobs':jobs},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'styles':len(STYLES),'images':len(jobs),'prompt_files':len(jobs)},ensure_ascii=False))


if __name__=='__main__':main()
