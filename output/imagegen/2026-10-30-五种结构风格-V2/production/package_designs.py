"""Package genuine generated images; derived contact sheets are previews only."""
from __future__ import annotations
from datetime import datetime, timezone
import hashlib
import html
import json
from pathlib import Path
import sys
import zipfile
from PIL import Image, ImageDraw, ImageFont, ImageOps

BASE=Path(__file__).resolve().parents[1]
FONT=Path('C:/Windows/Fonts/msyh.ttc')
NAMES={'home':'小程序 · 市场首页','detail':'小程序 · 服务详情','web':'Web · 算力工作台'}
TYPE_NAMES={'01':'新拟物','02':'毛玻璃','03':'新扁平','04':'新粗野主义','05':'极简目录'}
LAYOUTS={
    '01':'凹凸控件 / 居中设备展示 / 桌面窄导航轨',
    '02':'半透明浮层 / 非对称资源分区 / 桌面顶部悬浮导航',
    '03':'纯色二维 / 分类四宫格 / 紧凑资源比较行',
    '04':'粗黑描边 / 硬偏移阴影 / 票卡与交易表',
    '05':'开放留白 / 无框产品目录 / 文字索引与开放报价',
}

def font(size):return ImageFont.truetype(str(FONT),size)

def inspect(job):
    path=BASE/job['output']
    if not path.is_file():raise RuntimeError('Missing output: '+str(path))
    with Image.open(path) as img:img.verify()
    with Image.open(path) as img:
        img.load();width,height=img.size
        if f'{width}x{height}'!=job['size']:raise RuntimeError('Unexpected dimensions: '+str(path))
    return dict(id=job['id'],family=job['style'],page=job['page'],path=path.relative_to(BASE).as_posix(),width=width,height=height,bytes=path.stat().st_size,sha256=hashlib.sha256(path.read_bytes()).hexdigest(),model=job['model'],quality=job['quality'],reference=None if not job['reference'] else Path(job['reference']).as_posix(),reference_sha256=None if not job['reference'] else hashlib.sha256((BASE/job['reference']).read_bytes()).hexdigest())

def image_on(canvas,path,x,y,w,h):
    with Image.open(path) as src:
        scaled=ImageOps.contain(src.convert('RGB'),(w,h),Image.Resampling.LANCZOS)
        canvas.paste(scaled,(x+(w-scaled.width)//2,y+(h-scaled.height)//2))

def home_preview(jobs,preview):
    preview.mkdir(exist_ok=True)
    overview=Image.new('RGB',(1744,764),'#E9EEF3');draw=ImageDraw.Draw(overview)
    draw.text((32,17),'Token Share Mesh · 五种布局与材质方向',font=font(30),fill='#172B3A')
    draw.text((32,61),'五套独立设计语言；每套另有服务详情与Web页。以下为首页缩略对比。',font=font(19),fill='#435564')
    for index,number in enumerate(TYPE_NAMES):
        x=32+index*344
        draw.text((x,112),number+'  '+TYPE_NAMES[number],font=font(21),fill='#172B3A')
        home=next(j for j in jobs if j['style']==number and j['page']=='home')
        image_on(overview,BASE/home['output'],x,154,304,456)
        draw.text((x,633),home['style_name'].split('-')[0],font=font(20),fill='#314957')
    draw.text((32,718),'AI视觉概念稿 · 示例设备与价格 · 非已上线页面',font=font(17),fill='#546574')
    overview.save(preview/'00-五种首页对比.png')

def main():
    jobs=json.loads((BASE/'production/jobs.json').read_text(encoding='utf-8'))['jobs']
    if len(jobs)!=15:raise RuntimeError('Expected exactly 15 jobs')
    if '--homes-only' in sys.argv:
        home_preview(jobs,BASE/'previews')
        print('HOME_PREVIEW_CREATED: previews/00-五种首页对比.png')
        return
    records=[inspect(j) for j in jobs]
    review_path=BASE/'production/visual-review.json'
    if not review_path.is_file():raise RuntimeError('Actual visual review must be recorded first')
    review=json.loads(review_path.read_text(encoding='utf-8'))
    if set(review.get('reviewed_ids',[]))!={j['id'] for j in jobs}:raise RuntimeError('Visual review does not cover every image')
    preview=BASE/'previews';preview.mkdir(exist_ok=True)
    for number in TYPE_NAMES:
        group=[j for j in jobs if j['style']==number]
        canvas=Image.new('RGB',(1744,760),'#E9EEF3');draw=ImageDraw.Draw(canvas)
        title=f'{number}  {group[0]["style_name"].replace("-"," · ")}'
        draw.text((32,18),title,font=font(29),fill='#172B3A')
        draw.text((32,58),LAYOUTS[number],font=font(19),fill='#435564')
        positions=[(32,384),(440,384),(848,864)]
        for job,(x,w) in zip(group,positions):
            draw.text((x,99),NAMES[job['page']],font=font(20),fill='#314957')
            image_on(canvas,BASE/job['output'],x,132,w,576)
        draw.text((32,722),'Token Share Mesh  /  AI视觉概念稿 · 示例数据  /  V2',font=font(16),fill='#546574')
        canvas.save(preview/(number+'-三页总览.png'))
    home_preview(jobs,preview)
    manifest=dict(revision='V2',created_at=datetime.now(timezone.utc).isoformat(),main_image_count=15,concept_only=True,image_provider='User-invoked sub2-image-gen skill',model='gpt-image-2.5',browser_used=False,virtualization_used=False,development_goal='paused',file_checks='All 15 PNGs fully decoded, dimensions and SHA-256 recorded',visual_review='production/visual-review.json',images=records)
    (BASE/'delivery-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    lines=['# Token Share Mesh · 五种跨端设计 V2','',
        '共15张主稿：每套2张小程序、1张Web。源码开发保持暂停。所有图均为Sub2API / gpt-image-2.5生成的视觉概念稿，使用示例资源、卖家与价格。',
        '', '手机主标题统一为“摩托罗拉 X30”，模型“Qwen 3B”。旧版初稿不参与本次交付。',
        '', '![五种首页对比](previews/00-五种首页对比.png)',
        '', '| 风格 | 布局与材质 | 小程序首页 | 小程序详情 | Web | 三页总览 |','| --- | --- | --- | --- | --- | --- |']
    sections=[]
    for number in TYPE_NAMES:
        group=[j for j in jobs if j['style']==number]
        links=['[原图]('+Path(j['output']).as_posix()+')' for j in group]
        lines.append('| '+group[0]['style_name'].replace('-',' · ')+' | '+LAYOUTS[number]+' | '+' | '.join(links)+' | [总览](previews/'+number+'-三页总览.png) |')
        sections+=['','## '+group[0]['style_name'].replace('-',' · '),'','![三页总览](previews/'+number+'-三页总览.png)']
    lines+=['','[完整风格隔离与跨端一致性约束](00-风格隔离与跨端一致性约束.md)','',
        '[逐图评审记录](production/visual-review.json) · [文件清单与校验值](delivery-manifest.json)',
        '', '小程序原图1024×1536；Web原图1536×1024；PNG、high质量。总览由真实原图缩小拼接，原图未被替换成程序绘制的界面。',
        '', '已加载：',
        '', '- [Anthropic frontend-design](https://github.com/anthropics/skills/tree/main/skills/frontend-design)，综合技能仓库快照184754星。',
        '- [UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill)，本次检索的设计专用候选中最高，快照142473星。',
        '', '快照只代表2026-10-30读取时的整个仓库，不代表单个技能独立获得相应星标。技能副本、样式记录、提示词和生成结果在production/和各风格prompts/中可追溯。',
        '', '图片用于选风格和检查组内一致性。生成字体、图标与间距仍需在选定方向后转为精确组件；本轮不宣称已通过交互、读屏、支付、设备与加密的产品验收。']
    lines+=sections
    (BASE/'README.md').write_text('\n'.join(lines)+'\n',encoding='utf-8')
    blocks=[]
    for number in TYPE_NAMES:
        group=[j for j in jobs if j['style']==number]
        items=[]
        for j in group:
            path=Path(j['output']).as_posix();label=NAMES[j['page']]
            items.append('<a target="_blank" href="'+html.escape(path,quote=True)+'"><span>'+label+'</span><img loading="lazy" alt="'+html.escape(group[0]['style_name']+' '+label,quote=True)+'" src="'+html.escape(path,quote=True)+'"></a>')
        blocks.append('<section id="s'+number+'"><h2>'+html.escape(group[0]['style_name'].replace('-',' · '))+'</h2><p>'+html.escape(LAYOUTS[number])+'</p><div class="pages">'+''.join(items)+'</div></section>')
    viewer='''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>五种跨端设计 V2 · Token Share Mesh</title><style>body{margin:0;background:#edf0f4;color:#173044;font:16px/1.65 "Microsoft YaHei",sans-serif}header,main{max-width:1720px;margin:auto;padding:24px}h1{margin:0;font-size:30px}header p{margin:6px 0}nav{display:flex;gap:20px;flex-wrap:wrap;margin:20px 0}a{color:#285bb0}section{margin:12px 0 42px;scroll-margin-top:16px}h2{margin-bottom:4px}section p{margin-top:0;color:#496070}.pages{display:grid;grid-template-columns:1fr 1fr 2.25fr;gap:18px;align-items:start}.pages a{display:block;text-decoration:none}.pages span{display:block;margin-bottom:8px}.pages img{width:100%;height:auto;display:block;box-shadow:0 3px 14px #18304418}@media(max-width:800px){.pages{grid-template-columns:1fr 1fr}.pages a:last-child{grid-column:1/-1}header,main{padding:16px}}</style><header><h1>Token Share Mesh · 五种独立设计语言</h1><p>15张AI视觉概念稿 · 2张小程序 + 1张Web / 套 · 点击图片查看原图</p><p>手机型号：摩托罗拉 X30。资源与价格为示例；源码开发保持暂停。</p><nav>'''
    viewer+=''.join('<a href="#s'+n+'">'+n+' '+label+'</a>' for n,label in TYPE_NAMES.items())+'</nav></header><main>'+''.join(blocks)+'</main></html>'
    (BASE/'设计稿总览.html').write_text(viewer,encoding='utf-8')
    package=BASE/'Token-Share-Mesh-5-styles-V2.zip'
    members=[BASE/r['path'] for r in records]+list(preview.glob('*.png'))+[BASE/'README.md',BASE/'00-风格隔离与跨端一致性约束.md',BASE/'delivery-manifest.json',BASE/'设计稿总览.html',review_path]
    members+=list(BASE.glob('0*/design-system.json'))+list(BASE.glob('0*/prompts/*.txt'))
    members+=list((BASE/'production/research').glob('*.json'))
    with zipfile.ZipFile(package,'w',zipfile.ZIP_DEFLATED,compresslevel=5) as z:
        for member in members:z.write(member,member.relative_to(BASE).as_posix())
    print(json.dumps(dict(status='PACKAGED',main_images=15,previews=6,archive=str(package),archive_bytes=package.stat().st_size),ensure_ascii=False))

if __name__=='__main__':main()
