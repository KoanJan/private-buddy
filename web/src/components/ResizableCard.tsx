import { useState, useRef, useCallback, type ReactNode } from 'react';
import './ResizableCard.css';

interface ResizableCardProps {
  children: ReactNode;
  defaultWidth?: number;
  minWidth?: number;
  maxWidth?: number;
  resizeSide?: 'left' | 'right';
  flex?: boolean;
  className?: string;
}

/** Card component with a draggable resize handle on one side. */
export default function ResizableCard({
  children,
  defaultWidth = 480,
  minWidth = 200,
  maxWidth = 800,
  resizeSide = 'left',
  flex = false,
  className = '',
}: ResizableCardProps) {
  const [width, setWidth] = useState(defaultWidth);
  const widthRef = useRef(defaultWidth);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();

    const startX = e.clientX;
    const startWidth = widthRef.current;
    const direction = resizeSide === 'left' ? 1 : -1;

    const handleMouseMove = (moveEvent: MouseEvent) => {
      const delta = (startX - moveEvent.clientX) * direction;
      const newWidth = Math.min(maxWidth, Math.max(minWidth, startWidth + delta));
      widthRef.current = newWidth;
      setWidth(newWidth);
    };

    const handleMouseUp = () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.body.style.cursor = 'ew-resize';
    document.body.style.userSelect = 'none';
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
  }, [resizeSide, minWidth, maxWidth]);

  const panelStyle = flex ? { flex: 1 } : { width };

  return (
    <div className={`resizable-panel ${className}`} style={panelStyle}>
      <div className="resizable-card">
        {!flex && (
          <div
            className={`resize-handle resize-handle-${resizeSide}`}
            onMouseDown={handleMouseDown}
          />
        )}
        {children}
      </div>
    </div>
  );
}
