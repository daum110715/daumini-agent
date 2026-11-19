import { cubicOut, cubicIn } from 'svelte/easing';
import type { TransitionConfig } from 'svelte/transition';

function prefersReduced(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
}

export function pop(
  _node: Element,
  params: { y?: number; start?: number; blur?: number; duration?: number } = {}
): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const { y = -6, start = 0.95, blur = 4, duration = 210 } = params;
  return {
    duration,
    easing: cubicOut,
    css: (t, u) => {
      const s = start + (1 - start) * t;
      const dy = y * u;
      const b = blur * u;
      return `opacity: ${t}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)}); filter: blur(${b.toFixed(2)}px);`;
    }
  };
}

export function popOut(
  _node: Element,
  params: { y?: number; end?: number; blur?: number; duration?: number } = {}
): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const { y = -4, end = 0.97, blur = 2, duration = 130 } = params;
  return {
    duration,
    easing: cubicIn,
    css: (t, u) => {
      const s = end + (1 - end) * t;
      const dy = y * u;
      const b = blur * u;
      return `opacity: ${t}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)}); filter: blur(${b.toFixed(2)}px);`;
    }
  };
}

export function dialogIn(_node: Element): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  return {
    duration: 280,
    easing: cubicOut,
    css: (t, u) => {
      const s = 0.94 + 0.06 * t;
      const dy = 12 * u;
      const b = 6 * u;
      return `opacity: ${t}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)}); filter: blur(${b.toFixed(2)}px);`;
    }
  };
}

export function turnIn(_node: Element, params: { y?: number; duration?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const { y = 8, duration = 240 } = params;
  return {
    duration,
    easing: cubicOut,
    css: (t, u) => `opacity: ${t}; transform: translate3d(0, ${(y * u).toFixed(2)}px, 0);`
  };
}
