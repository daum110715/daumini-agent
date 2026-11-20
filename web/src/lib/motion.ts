import { cubicIn, cubicOut } from 'svelte/easing';
import type { TransitionConfig } from 'svelte/transition';
import { getActiveStyle } from './themes';

function prefersReduced(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
}

// Atelier fluid silk curve: fast luminous launch, long velvety magnetic settle.
function silkOut(t: number): number {
  return 1 - Math.pow(1 - t, 4.2);
}

// Atelier aperture retract: crisp, light, zero heaviness.
function silkIn(t: number): number {
  return Math.pow(t, 2.2);
}

// Sketch theme deceleration (paper & ink bounce).
function sketchOut(t: number): number {
  return cubicOut(t);
}

/**
 * Floating popovers, menus, inline banners, toasts.
 * - atelier: directional optical curtain reveal (clip-path aperture + subtle silk drift, zero blur/bounce)
 * - sketch:  paper pop (scale + translateY + gaussian blur)
 */
export function pop(
  _node: Element,
  params: { y?: number; start?: number; blur?: number; duration?: number } = {}
): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();

  if (style === 'atelier') {
    const upward = (params.y ?? -6) > 0;
    const duration = params.duration ? Math.min(260, Math.round(params.duration * 1.15)) : 230;
    return {
      duration,
      easing: silkOut,
      css: (t, u) => {
        const clip = (u * 100).toFixed(2);
        const clipPath = upward
          ? `inset(${clip}% 0 0 0 round 12px)`
          : `inset(0 0 ${clip}% 0 round 12px)`;
        const dy = (upward ? 7 : -7) * u;
        const opacity = Math.min(1, t * 1.6);
        return `opacity: ${opacity.toFixed(3)}; clip-path: ${clipPath}; transform: translate3d(0, ${dy.toFixed(2)}px, 0);`;
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
    const upward = (params.y ?? -4) > 0;
    return {
      duration: 140,
      easing: silkIn,
      css: (t, u) => {
        const clip = (u * 85).toFixed(2);
        const clipPath = upward
          ? `inset(${clip}% 0 0 0 round 12px)`
          : `inset(0 0 ${clip}% 0 round 12px)`;
        const dy = (upward ? 5 : -5) * u;
        return `opacity: ${t.toFixed(3)}; clip-path: ${clipPath}; transform: translate3d(0, ${dy.toFixed(2)}px, 0);`;
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

/**
 * Modal dialogs.
 * - atelier: 3D architectural perspective unfold + lower-edge optical aperture reveal
 * - sketch:  springy scale + blur fade-in
 */
export function dialogIn(_node: Element): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();

  if (style === 'atelier') {
    return {
      duration: 320,
      easing: silkOut,
      css: (t, u) => {
        const rotX = (4.2 * u).toFixed(2);
        const dy = (22 * u).toFixed(2);
        const dz = (-24 * u).toFixed(2);
        const clipBottom = (16 * u).toFixed(2);
        const opacity = Math.min(1, t * 1.75);
        return `opacity: ${opacity.toFixed(3)}; clip-path: inset(0 0 ${clipBottom}% 0 round 20px); transform: perspective(1000px) translate3d(0, ${dy}px, ${dz}px) rotateX(${rotX}deg); transform-origin: 50% 18%;`;
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

/**
 * Side inspector drawer.
 * - atelier: shoji-screen horizontal aperture wipe + gentle parallax glide
 * - sketch:  direct slide-in from right
 */
export function drawerIn(_node: Element, params: { x?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();
  const x = params.x ?? 24;

  if (style === 'atelier') {
    return {
      duration: 310,
      easing: silkOut,
      css: (t, u) => {
        const clipLeft = (u * 100).toFixed(2);
        const dx = (14 * u).toFixed(2);
        const opacity = Math.min(1, t * 1.55);
        return `opacity: ${opacity.toFixed(3)}; clip-path: inset(0 0 0 ${clipLeft}%); transform: translate3d(${dx}px, 0, 0);`;
      }
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
      duration: 190,
      easing: silkIn,
      css: (t, u) => {
        const clipLeft = (u * 100).toFixed(2);
        const dx = (10 * u).toFixed(2);
        return `opacity: ${t.toFixed(3)}; clip-path: inset(0 0 0 ${clipLeft}%); transform: translate3d(${dx}px, 0, 0);`;
      }
    };
  }

  return {
    duration: 170,
    easing: cubicIn,
    css: (t, u) => `opacity: ${t}; transform: translate3d(${(x * u).toFixed(2)}px, 0, 0);`
  };
}

/**
 * Conversation turns & streaming status blocks.
 * - atelier: horizontal-skewed silk unveil with soft clip-path
 * - sketch:  vertical rise fade-in
 */
export function turnIn(_node: Element, params: { y?: number; duration?: number } = {}): TransitionConfig {
  if (prefersReduced()) return { duration: 0 };
  const style = getActiveStyle();

  if (style === 'atelier') {
    return {
      duration: params.duration ? Math.round(params.duration * 1.2) : 250,
      easing: silkOut,
      css: (t, u) => {
        const dx = (-10 * u).toFixed(2);
        const clipBottom = (22 * u).toFixed(2);
        const opacity = Math.min(1, t * 1.5);
        return `opacity: ${opacity.toFixed(3)}; clip-path: inset(0 0 ${clipBottom}% 0 round 10px); transform: translate3d(${dx}px, 0, 0);`;
      }
    };
  }

  const { y = 8, duration = 240 } = params;
  return {
    duration,
    easing: sketchOut,
    css: (t, u) => `opacity: ${t}; transform: translate3d(0, ${(y * u).toFixed(2)}px, 0);`
  };
}
