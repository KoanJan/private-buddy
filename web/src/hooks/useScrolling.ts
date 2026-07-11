import { useEffect } from 'react';

const TIMEOUT = 800;
const FADE_DURATION = 500;
const ALPHA_MAX = 0.15;

const timers = new WeakMap<Element, ReturnType<typeof setTimeout>>();
const animationFrames = new WeakMap<Element, number>();

function fadeIn(el: HTMLElement) {
  const existing = animationFrames.get(el);
  if (existing) cancelAnimationFrame(existing);

  el.style.setProperty('--scrollbar-alpha', String(ALPHA_MAX));
}

function fadeOut(el: HTMLElement) {
  const startTime = performance.now();
  const startAlpha = parseFloat(el.style.getPropertyValue('--scrollbar-alpha') || '0');

  const animate = (now: number) => {
    const elapsed = now - startTime;
    const progress = Math.min(elapsed / FADE_DURATION, 1);
    const alpha = startAlpha * (1 - progress);

    el.style.setProperty('--scrollbar-alpha', String(alpha));

    if (progress < 1) {
      animationFrames.set(el, requestAnimationFrame(animate));
    } else {
      el.style.removeProperty('--scrollbar-alpha');
      animationFrames.delete(el);
    }
  };

  animationFrames.set(el, requestAnimationFrame(animate));
}

/** Hook that adds auto-fading scrollbar visibility on scroll events. */
export default function useScrolling(): void {
  useEffect(() => {
    const handleScroll = (e: Event) => {
      const target = e.target as Element;
      if (!target || !target.classList) return;

      const el = target instanceof HTMLElement ? target : null;
      if (!el) return;

      fadeIn(el);

      const existing = timers.get(el);
      if (existing) clearTimeout(existing);

      timers.set(el, setTimeout(() => {
        fadeOut(el);
        timers.delete(el);
      }, TIMEOUT));
    };

    document.addEventListener('scroll', handleScroll, { passive: true, capture: true });

    return () => {
      document.removeEventListener('scroll', handleScroll, { capture: true });
    };
  }, []);
}
