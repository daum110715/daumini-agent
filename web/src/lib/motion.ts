import { cubicIn, cubicOut } from 'svelte/easing';
import type { TransitionConfig } from 'svelte/transition';
import { getActiveStyle } from './themes';

function prefersReduced(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
}

// Critically-damped exponential deceleration for the Atelier theme (silky glide, zero overshoot jitter).
function expoOut(t: number): number {
  return t === 1 ? 1 : 1 - Math.pow(2, -10 * t);
}

function quadIn(t: number): number {
  return t * t;
}

// Slightly springy deceleration for the Sketch theme.
function sketchOut(t: number): number {
  return cubicOut(t);
}

export function pop(
  _node: Element,
  params: { y?: number; start?: number; blur?: number; duration?: number } = {}
): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();

  if (style === 'atelier') {
    const dyMax = params.y !== undefined ? Math.sign(params.y) * Math.min(Math.abs(params.y), 5) : -4;
    const duration = params.duration ? Math.round(params.duration * 0.92) : 190;
    return {
      duration,
      easing: expoOut,
      css: (t, u) => {
        const s = 0.984 + 0.016 * t;
        const dy = dyMax * u;
        return `opacity: ${Math.min(1, t * 1.25).toFixed(3)}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)});`;
      }
    };
  }

  const { y = -6, start = 0.95, blur = 4, duration = 210 } = params;
  return {
    duration,
    easing: sketchOut,
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
  const style = getActiveStyle();

  if (style === 'atelier') {
    const dyMax = params.y !== undefined ? Math.sign(params.y) * Math.min(Math.abs(params.y), 3) : -2;
    return {
      duration: 115,
      easing: quadIn,
      css: (t, u) => {
        const s = 0.988 + 0.012 * t;
        return `opacity: ${t.toFixed(3)}; transform: translate3d(0, ${(dyMax * u).toFixed(2)}px, 0) scale(${s.toFixed(4)});`;
      }
    };
  }

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
  const style = getActiveStyle();

  if (style === 'atelier') {
    return {
      duration: 240,
      easing: expoOut,
      css: (t, u) => {
        const s = 0.976 + 0.024 * t;
        const dy = 12 * u;
        return `opacity: ${Math.min(1, t * 1.3).toFixed(3)}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)});`;
      }
    };
  }

  return {
    duration: 280,
    easing: sketchOut,
    css: (t, u) => {
      const s = 0.94 + 0.06 * t;
      const dy = 12 * u;
      const b = 6 * u;
      return `opacity: ${t}; transform: translate3d(0, ${dy.toFixed(2)}px, 0) scale(${s.toFixed(4)}); filter: blur(${b.toFixed(2)}px);`;
    }
  };
}

export function drawerIn(_node: Element, params: { x?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();
  const x = params.x ?? 24;

  if (style === 'atelier') {
    return {
      duration: 260,
      easing: expoOut,
      css: (t, u) => `opacity: ${Math.min(1, t * 1.25).toFixed(3)}; transform: translate3d(${(x * u).toFixed(2)}px, 0, 0);`
    };
  }

  return {
    duration: 250,
    easing: sketchOut,
    css: (t, u) => `opacity: ${t}; transform: translate3d(${(x * u).toFixed(2)}px, 0, 0);`
  };
}

export function drawerOut(_node: Element, params: { x?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();
  const x = params.x ?? 18;

  if (style === 'atelier') {
    return {
      duration: 160,
      easing: quadIn,
      css: (t, u) => `opacity: ${t.toFixed(3)}; transform: translate3d(${(x * u).toFixed(2)}px, 0, 0);`
    };
  }

  return {
    duration: 170,
    easing: cubicIn,
    css: (t, u) => `opacity: ${t}; transform: translate3d(${(x * u).toFixed(2)}px, 0, 0);`
  };
}

export function turnIn(_node: Element, params: { y?: number; duration?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();

  if (style === 'atelier') {
    const y = Math.min(params.y ?? 6, 6);
    return {
      duration: params.duration ?? 210,
      easing: expoOut,
      css: (t, u) => `opacity: ${Math.min(1, t * 1.2).toFixed(3)}; transform: translate3d(0, ${(y * u).toFixed(2)}px, 0);`
    };
  }

  const { y = 8, duration = 240 } = params;
  return {
    duration,
    easing: sketchOut,
    css: (t, u) => `opacity: ${t}; transform: translate3d(0, ${(y * u).toFixed(2)}px, 0);`
  };
}
