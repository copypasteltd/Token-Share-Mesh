import { useEffect, type PropsWithChildren } from 'react';
import { model } from './runtime';
import '@tsm/core/tokens.css';
import './app.css';
export default function App({ children }: PropsWithChildren) {
  useEffect(() => { void model.start(); }, []);
  return children;
}
