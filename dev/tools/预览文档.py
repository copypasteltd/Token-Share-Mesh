"""仅在本机预览已提交文档；不开放runtime、.git或目录浏览。"""
from __future__ import annotations

import argparse
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
from urllib.parse import quote, unquote, urlsplit

ROOT = Path(__file__).resolve().parents[2]
WHITEPAPER = 'docs/白皮书/算力共享平台完整阅读版-v3.0.html'
TECHNICAL = 'docs/技术开发/技术开发文档完整阅读版-v1.0.html'


class ReaderHandler(SimpleHTTPRequestHandler):
    def target(self):
        path = unquote(urlsplit(self.path).path).replace('\\', '/')
        parts = [part for part in path.split('/') if part]
        if not parts or any(part in {'.', '..'} or part.startswith('.') for part in parts):
            return None
        candidate = (ROOT / Path(*parts)).resolve()
        if not candidate.is_relative_to(ROOT):
            return None
        allowed = [ROOT / 'docs', ROOT / 'dev/media']
        return candidate if any(candidate.is_relative_to(base) for base in allowed) else None

    def translate_path(self, _):
        target = self.target()
        return str(target) if target else str(ROOT / 'runtime/forbidden-preview-path')

    def send_head(self):
        if urlsplit(self.path).path == '/':
            self.send_response(302)
            self.send_header('Location', '/' + quote(WHITEPAPER, safe='/'))
            self.end_headers()
            return None
        target = self.target()
        if target is None:
            self.send_error(403, 'Only public documentation files are available')
            return None
        if target.is_dir():
            self.send_error(403, 'Directory listing is disabled')
            return None
        return super().send_head()

    def end_headers(self):
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.send_header('Cache-Control', 'no-store')
        super().end_headers()

    def log_message(self, *_):
        pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8765)
    args = parser.parse_args()
    with ThreadingHTTPServer(('127.0.0.1', args.port), ReaderHandler) as server:
        base = f'http://127.0.0.1:{server.server_port}/'
        print(json.dumps({'base_url': base, 'whitepaper': base + quote(WHITEPAPER, safe='/'), 'technical': base + quote(TECHNICAL, safe='/')}, ensure_ascii=False), flush=True)
        try:
            server.serve_forever(poll_interval=0.2)
        except KeyboardInterrupt:
            pass


if __name__ == '__main__':
    main()

