import { useId } from "react";

// Símbolo do SessionFlow (o mesmo de packaging/icons/sessionflow.svg): um
// fluxo ligando duas sessões, no gradiente sky→violet do app.
export function AppLogo({ className }: { className?: string }) {
  const id = useId();
  return (
    <svg viewBox="0 0 512 512" className={className} aria-hidden>
      <defs>
        <linearGradient id={`${id}-bg`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#1a2033" />
          <stop offset="1" stopColor="#0b0d14" />
        </linearGradient>
        <linearGradient id={`${id}-flow`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#38bdf8" />
          <stop offset="1" stopColor="#a78bfa" />
        </linearGradient>
      </defs>
      <rect x="16" y="16" width="480" height="480" rx="112" fill={`url(#${id}-bg)`} />
      <rect x="17.5" y="17.5" width="477" height="477" rx="110.5" fill="none" stroke="#fff" strokeOpacity="0.1" strokeWidth="6" />
      <path
        d="M152 164 H300 C368 164 380 244 318 256 L194 256 C132 268 144 348 212 348 H360"
        fill="none"
        stroke={`url(#${id}-flow)`}
        strokeWidth="48"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="152" cy="164" r="36" fill="#38bdf8" />
      <circle cx="360" cy="348" r="36" fill="#a78bfa" />
      <circle cx="152" cy="164" r="13" fill="#0b0d14" />
      <circle cx="360" cy="348" r="13" fill="#0b0d14" />
    </svg>
  );
}
