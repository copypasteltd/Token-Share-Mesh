import { useEffect, type PropsWithChildren } from 'react';
import { useDidHide } from '@tarojs/taro';
import { model } from './runtime';
import '@tsm/core/tokens.css';
import './app.css';
export default function App({ children }: PropsWithChildren) {
  useDidHide(model.clearPairingSecret);
  useEffect(() => { void model.start(); }, []);
  return children;
}
