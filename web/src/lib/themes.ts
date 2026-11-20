import type { UIStyle } from './types';

export interface ThemePack {
  id: UIStyle;
  name: string;
  tagline: string;
  motionTag: string;
  preview: {
    light: [string, string, string, string];
    dark: [string, string, string, string];
  };
}

export const THEME_PACKS: ThemePack[] = [
  {
    id: 'atelier',
    name: '静谧工作台',
    tagline: '温润平面层级 适合长时阅读编码',
    motionTag: '流体阻尼动效',
    preview: {
      light: ['#F6F5F1', '#FFFFFF', '#3B5BDB', '#2B8A3E'],
      dark: ['#131417', '#1D2026', '#748FFC', '#51CF66']
    }
  },
  {
    id: 'sketch',
    name: '手绘白板',
    tagline: '点阵纸面 9-slice 墨线与荧光标记',
    motionTag: '纸笔弹性动效',
    preview: {
      light: ['#FFFFFF', '#FFF3BF', '#1971C2', '#E03131'],
      dark: ['#121212', '#3B3417', '#4DABF7', '#FF6B6B']
    }
  }
];

export function isValidUIStyle(v: unknown): v is UIStyle {
  return v === 'atelier' || v === 'sketch';
}

export function getActiveStyle(): UIStyle {
  if (typeof document === 'undefined') return 'atelier';
  const attr = document.documentElement.dataset.style;
  return isValidUIStyle(attr) ? attr : 'atelier';
}
