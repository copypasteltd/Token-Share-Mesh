"""Verify and package final design artifacts; no browser, product execution or deployment."""
from __future__ import annotations
from datetime import datetime,timezone
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import re
import zipfile
from PIL import Image,ImageDraw,ImageFont,ImageOps

ROOT=Path(__file__).resolve().parents[3]
DOC=ROOT/'dev/design/新粗野主义-v1.0'
OUT=ROOT/'output/imagegen/2026-10-30-04-最终设计约束'
FONT='C:/Windows/Fonts/msyh.ttc'

def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def font(n):return ImageFont.truetype(FONT,n)
def rel(p):return p.relative_to(ROOT).as_posix()
def pic(canvas,path,x,y,w,h):
    with Image.open(path) as im:
        im=ImageOps.contain(im.convert('RGB'),(w,h),Image.Resampling.LANCZOS)
        canvas.paste(im,(x+(w-im.width)//2,y+(h-im.height)//2))

def local_links(files):
    failures=[];checked=0
    for p in files:
        for target in re.findall(r'\]\(([^)]+)\)',p.read_text(encoding='utf-8')):
            if target.startswith(('https://','http://','#','mailto:')):continue
            target=target.split('#',1)[0].strip('<>')
            if not target:continue
            resolved=(p.parent/target).resolve()
            checked+=1
            if not resolved.exists():failures.append(dict(file=rel(p),target=target))
    return dict(checked=checked,broken=failures)

def contrast(a,b):
    def lum(c):
        rgb=[int(c[i:i+2],16)/255 for i in (1,3,5)]
        linear=[v/12.92 if v<=0.04045 else ((v+0.055)/1.055)**2.4 for v in rgb]
        return sum(x*w for x,w in zip(linear,[0.2126,0.7152,0.0722]))
    x,y=sorted([lum(a),lum(b)])
    return round((y+0.05)/(x+0.05),2)

def main():
    progress=json.loads((ROOT/'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    goal_status=progress['goal_status']
    if goal_status not in {'ACTIVE','PAUSED','COMPLETE','BLOCKED'}:raise RuntimeError('Unknown source goal status')
    jobs=json.loads((OUT/'production/jobs.json').read_text(encoding='utf-8'))['jobs']
    if len(jobs)!=5 or {j['id'] for j in jobs}!={f'UI-P0{i}' for i in range(1,6)}:raise RuntimeError('Exactly UI-P01 through UI-P05 required')
    review=json.loads((OUT/'production/visual-review.json').read_text(encoding='utf-8'))
    if set(review['reviewed_ids'])!={j['id'] for j in jobs} or review['blocking_visual_defects_remaining']:raise RuntimeError('Visual review incomplete')
    token=json.loads((DOC/'design-tokens.json').read_text(encoding='utf-8'))
    seed=json.loads((DOC/'示例数据.json').read_text(encoding='utf-8'))
    if token['units']['tokens_per_M']!=1000000 or token['units']['gpu_price']!='小时' or token['units']['llm_price']!='M token':raise RuntimeError('Wrong billing units')
    if seed['gpu']['sample_total_fen']!=seed['gpu']['sample_seller_net_fen']+seed['gpu']['sample_platform_commission_fen']:raise RuntimeError('Quote sum mismatch')
    if Fraction(seed['gpu']['hourly_price_fen']*seed['rental_order']['elapsed_seconds'],3600)!=seed['rental_order']['current_fee_fen']:raise RuntimeError('Hourly example mismatch')
    if seed['rental_order']['current_fee_fen']-seed['rental_order']['current_fee_fen']*seed['commission']['basis_points']//10000!=seed['provider']['estimated_net_fen']:raise RuntimeError('Provider example mismatch')
    if seed['market_phone']['seller']==seed['provider_phone_draft']['seller']:raise RuntimeError('Demo ownership unexpectedly conflated')
    rows=[]
    for job in jobs:
        path=ROOT/job['output']
        with Image.open(path) as im:im.verify()
        with Image.open(path) as im:
            im.load();size=f'{im.width}x{im.height}'
        if size!=job['size']:raise RuntimeError('Wrong PNG size: '+job['id'])
        rows.append(dict(id=job['id'],name=job['name'],path=rel(path),size=size,bytes=path.stat().st_size,sha256=sha(path),reference=Path(job['reference']).as_posix(),reference_sha256=sha(ROOT/job['reference']),model='gpt-image-2.5',quality='high',visual_review='VISUALLY_REVIEWED'))
    preview=OUT/'previews';preview.mkdir(exist_ok=True)
    mini=Image.new('RGB',(1744,760),'#FFF5C5');d=ImageDraw.Draw(mini)
    d.text((32,18),'Token Share Mesh · 新粗野主义 · 小程序四页',font=font(30),fill='black')
    d.text((32,62),'NB-1.0  /  GPU按小时；LLM输入、输出按M token  /  AI视觉约束图',font=font(18),fill='#3A3A3A')
    for i,job in enumerate(jobs[:4]):
        x=32+i*432
        d.text((x,105),job['id']+' '+job['name'].replace('小程序-',''),font=font(20),fill='black')
        pic(mini,ROOT/job['output'],x,142,384,576)
    d.text((32,728),'具体型号与个人卖家 / 小时与百万token单位 / 概念数据，非已上线页面',font=font(15),fill='#3A3A3A')
    mini.save(preview/'01-小程序四页对比.png')
    board=Image.new('RGB',(1744,1850),'#FFF5C5');board.paste(mini,(0,0));bd=ImageDraw.Draw(board)
    bd.text((32,785),'UI-P05  Web算力工作台 · 同一设计系统',font=font(28),fill='black')
    pic(board,ROOT/jobs[4]['output'],152,840,1440,960)
    # The web source is 3:2, so 1440x960 is a faithful proportional preview.
    bd.text((32,1813),'NB-1.0 · AI视觉约束图，非已上线页面；正式实现遵循设计源文档与原生平台控件。',font=font(16),fill='#3A3A3A')
    board.save(preview/'00-五页约束总览.png')
    source_files=[DOC/'README.md']+sorted(DOC.glob('[0-9][0-9]-*.md'))
    if len(source_files)!=11:raise RuntimeError('Expected index and ten design chapters')
    combined='# Token Share Mesh · 新粗野主义完整设计规范 NB-1.0\n\n设计文档合订阅读版；原始章节和JSON保持权威。图为AI视觉概念，源码阶段见[实施登记](../../progress/README.md)。\n\n'
    for source in source_files:combined+='\n\n---\n\n'+source.read_text(encoding='utf-8')
    (DOC/'设计规范合订本.md').write_text(combined+'\n',encoding='utf-8')
    lines=['# 五张最终视觉约束图 · NB-1.0','',
      '用户选定04新粗野主义。四张小程序+一张Web；GPU价格按小时，LLM输入/输出按M token。具体型号、卖家、费率均为示例；源码阶段见[实施登记](../../../dev/progress/README.md)。','',
      '![五页约束总览](previews/00-五页约束总览.png)','',
      '| 页面 | 原图 | 尺寸 |','| --- | --- | --- |']
    for job in jobs:lines.append('| '+job['id']+' '+job['name']+' | [查看]('+Path(job['output']).name+') | '+job['size']+' |')
    lines+=['','[完整设计入口](../../../dev/design/新粗野主义-v1.0/README.md) · [合订本](../../../dev/design/新粗野主义-v1.0/设计规范合订本.md)','',
      '[设计令牌](../../../dev/design/新粗野主义-v1.0/design-tokens.json) · [示例数据](../../../dev/design/新粗野主义-v1.0/示例数据.json) · [逐张目视记录](production/visual-review.json)','',
      '图像仅通过用户指定sub2-image-gen、gpt-image-2.5、high、PNG生成。预览由五张真实生成图等比缩放拼合。P04补齐系统页头后复查；旧版只保存在历史，不计入五张最终主稿。','',
      '图片不代表交互、真机、支付、密码或性能已验收。细小生成字形/系统胶囊以文案与原生平台控件为准；设备插画不作为真实硬件证明。']
    (OUT/'README.md').write_text('\n'.join(lines)+'\n',encoding='utf-8')
    links=local_links(source_files+[DOC/'设计规范合订本.md',OUT/'README.md'])
    if links['broken']:raise RuntimeError('Broken design links: '+json.dumps(links['broken'],ensure_ascii=False))
    planned_contrast={k:contrast(token['color']['ink'],token['color'][k]) for k in ['canvas','surface','primary','selected']}
    checks=dict(design_version='NB-1.0',checked_at=datetime.now(timezone.utc).isoformat(),status='DESIGN_ARTIFACT_CHECKS_PASSED',checks=dict(exactly_five_images=True,full_png_decode=True,dimensions=True,visual_review_complete=True,design_chapters=11,json_sources_valid=True,units_hours_and_million_tokens=True,quote_sum_180_equals_171_plus_9=True,current_10_minutes_60_fen=True,provider_60_minus_3_equals_57=True,phone_ownership_separate=True,links=links,planned_token_contrast=planned_contrast),limits='Checks validate design artifacts and examples only. No product/runtime, real device, payment, cryptography, accessibility or performance acceptance.',development_goal=goal_status,browser_used=False,virtualization_used=False)
    (DOC/'artifact-checks.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    doc_files=source_files+[DOC/'design-tokens.json',DOC/'示例数据.json',DOC/'设计规范合订本.md',DOC/'artifact-checks.json']
    manifest=dict(design_version='NB-1.0',generated_at=datetime.now(timezone.utc).isoformat(),main_image_count=5,mini_program_pages=4,web_pages=1,concept_only=True,image_model='gpt-image-2.5',development_goal=goal_status,images=rows,documents=[dict(path=rel(p),sha256=sha(p),bytes=p.stat().st_size) for p in doc_files],visual_review='production/visual-review.json',refinements='production/refinements.json',system_chrome='Concept glyphs only; native Alipay chrome controls implementation',file_integrity_checks=rel(DOC/'artifact-checks.json'))
    (OUT/'delivery-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    archive=OUT/'Token-Share-Mesh-NB-1.0-设计交付.zip'
    members=[ROOT/j['output'] for j in jobs]+list(preview.glob('*.png'))+doc_files+[OUT/'README.md',OUT/'delivery-manifest.json',OUT/'production/visual-review.json',OUT/'production/refinements.json',OUT/'production/jobs.json']+list((OUT/'prompts').glob('*.txt'))
    with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED,compresslevel=5) as package:
        for p in members:package.write(p,rel(p))
    with zipfile.ZipFile(archive) as package:
        corrupt=package.testzip()
        if corrupt:raise RuntimeError('Corrupt archive member '+corrupt)
    print(json.dumps(dict(status='DESIGN_PACKAGE_READY',images=5,chapters=11,documents=len(doc_files),links_checked=links['checked'],broken_links=0,archive=rel(archive),archive_bytes=archive.stat().st_size),ensure_ascii=False))

if __name__=='__main__':main()
