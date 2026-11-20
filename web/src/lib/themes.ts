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
    name: '晴空工坊',
    tagline: '清透瓷白与星夜深海 无框色块光影层次',
    motionTag: '光学揭幕与流光动效',
    preview: {
      light: ['#FAFBFF', '#F1F5FB', '#4F46E5', '#0D9488'],
      dark: ['#0B1020', '#151E35', '#7C8CFF', '#2DD4BF']
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
