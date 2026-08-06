// Brand mark: a periscope breaking the surface, on a rounded tile. The tile
// carries its own background so the mark reads the same in every theme.
export function PeriscopeLogo({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 32 32"
      width="32"
      height="32"
      role="img"
      aria-hidden="true"
      focusable="false"
    >
      <defs>
        <linearGradient id="cc-brand-tile" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#2b9af3" />
          <stop offset="1" stopColor="#004b95" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill="url(#cc-brand-tile)" />
      {/* waterline, drawn first so the tube appears to pass through it */}
      <path
        d="M5 20q2.5-2.2 5 0t5 0t5 0t5 0"
        fill="none"
        stroke="#ffffff"
        strokeOpacity="0.4"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
      {/* tube and head */}
      <path
        d="M11.5 25V10.5h5"
        fill="none"
        stroke="#ffffff"
        strokeWidth="3.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {/* lens */}
      <circle cx="18.8" cy="10.5" r="2.9" fill="#ffffff" />
      <circle cx="18.8" cy="10.5" r="1.15" fill="#0e63b3" />
      {/* line of sight */}
      <path
        d="M23.2 8.2a5 5 0 0 1 0 4.6"
        fill="none"
        stroke="#ffffff"
        strokeOpacity="0.75"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <path
        d="M26 6.6a8.4 8.4 0 0 1 0 7.8"
        fill="none"
        stroke="#ffffff"
        strokeOpacity="0.45"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  )
}
