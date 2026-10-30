import { defineConfig } from '@tarojs/cli';
import path from 'node:path';

const projectRoot = process.env.P11_PROJECT_ROOT;
if (!projectRoot) throw new Error('Use dev/tools/前端开发.py so all dependencies and output stay inside runtime');
export default defineConfig({
  projectName: 'Token Share Mesh', date: '2026-10-30', designWidth: 375,
  deviceRatio: { 375: 2, 640: 1.17, 750: 1, 828: 0.905 },
  sourceRoot: 'src', outputRoot: path.join(projectRoot, 'runtime/frontend/builds/miniapp-' + process.env.TARO_ENV),
  framework: 'react', compiler: { type: 'webpack5', prebundle: { enable: false } },
  cache: { enable: false },
  plugins: ['@tarojs/plugin-framework-react', '@tarojs/plugin-platform-alipay', '@tarojs/plugin-platform-h5'],
  defineConstants: { P11_API_BASE: JSON.stringify(process.env.P11_FRONTEND_API_BASE || '') },
  mini: { compile: { include: [path.resolve(__dirname, '../../frontend/packages/core')] }, postcss: { pxtransform: { enable: true }, cssModules: { enable: false } } },
  h5: { publicPath: '/miniapp/', router: { mode: 'hash', basename: '/miniapp' },
    compile: { include: [path.resolve(__dirname, '../../frontend/packages/core')] },
    postcss: { pxtransform: { enable: true }, cssModules: { enable: false } } },
  copy: { patterns: [], options: {} }
});
