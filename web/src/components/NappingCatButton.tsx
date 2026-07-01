import { useCallback, useState } from 'react';
import catSvgRaw from '../assets/cat-icon.svg?raw';
import './NappingCatButton.css';

// Tail wag keyframes expressed as SVG path `d` values. Every shape uses the
// same command sequence (M c S) so SMIL interpolates smoothly between them —
// the tail actually bends through intermediate shapes instead of rotating as
// a rigid stick around its base.
//   shape 0: rest (matches the asset)
//   shape 1: tail curls left — mid-segment straightens, tip swings inward
//   shape 2: tail fans right — mid-segment bends more, tip swings outward
//   shape 3: back to rest
const TAIL_KEYFRAMES = [
  'M42.3,54.2c0-1-1.9-6.1,3.9-14.2S51,28.9,51,25.8',
  'M42.3,54.2c0-1,1.5-6.1,5.9-14.2S47,30,45,24',
  'M42.3,54.2c0-1-3.5-6.1,2.9-14.2S53,28,53,27',
  'M42.3,54.2c0-1-1.9-6.1,3.9-14.2S51,28.9,51,25.8',
].join(';');

// SMIL <animate> injected as a child of the tail path. begin="0s" plays once
// when the element is inserted; React remount (via the wagId key) re-inserts
// the whole SVG and replays the wag — same restart trick as with CSS animation.
const TAIL_ANIMATE =
  `<animate attributeName="d" dur="0.55s" begin="0s" ` +
  `values="${TAIL_KEYFRAMES}" keyTimes="0;0.35;0.7;1" ` +
  `calcMode="spline" ` +
  `keySplines="0.3 0 0.7 1; 0.3 0 0.7 1; 0.3 0 0.7 1"/>`;

// Build the inlined SVG string. The asset is recolored to currentColor so the
// cat picks up the button's tone. When `wag` is true, an <animate> is injected
// into the tail path (the only path originally carrying id="Shape") so it
// morphs through several bent shapes on this render — a true bend, not a
// rigid rotation. The asset file itself is never modified.
function buildCatSvg(wag: boolean): string {
  const svg = catSvgRaw
    .slice(catSvgRaw.indexOf('<svg'))
    .replace(/stroke="#6B6C6E"/g, 'stroke="currentColor"');
  if (!wag) return svg;
  // The tail path is self-closing in the asset; turn it into an open-close
  // pair and slip the <animate> inside.
  return svg.replace(
    /(<path id="Shape"[^>]*?)\/>/,
    `$1>${TAIL_ANIMATE}</path>`,
  );
}

interface NappingCatButtonProps {
  // Fired when the cat is clicked (a new big-view switch is triggered).
  onClick: () => void;
  // When true, clicks are ignored (e.g. while a slide is in progress).
  disabled?: boolean;
}

/**
 * Napping cat button.
 *
 * A line-art cat (SVG asset) used as the big-view ring switcher in the header.
 * Mostly static; on click the tail bends through a short damped wag. The SVG
 * is inlined (via Vite's ?raw), recolored with currentColor, and — on the click
 * render — an SMIL <animate> is injected into the tail path so its `d` morphs
 * through several bent shapes (a real bend, not a rigid rotation).
 */
export default function NappingCatButton({ onClick, disabled }: NappingCatButtonProps) {
  // Bumped on every click and used as the icon's React key. Each increment
  // forces a remount of the icon span, which re-inserts the SVG and replays
  // the SMIL tail wag — even on rapid successive clicks.
  const [wagId, setWagId] = useState(0);

  const handleClick = useCallback(() => {
    if (disabled) return;
    setWagId(id => id + 1);
    onClick();
  }, [disabled, onClick]);

  return (
    <button
      type="button"
      className="napping-cat-button"
      onClick={handleClick}
      disabled={disabled}
      aria-label="Switch view"
    >
      <span className="napping-cat-shape">
        <span
          key={wagId}
          className="napping-cat-icon"
          aria-hidden="true"
          dangerouslySetInnerHTML={{ __html: buildCatSvg(wagId > 0) }}
        />
      </span>
    </button>
  );
}
