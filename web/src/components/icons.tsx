import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

function Svg({ children, ...props }: IconProps) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  );
}

export const IconDashboard = (p: IconProps) => (
  <Svg {...p}>
    <rect x="3" y="3" width="7" height="9" rx="1.5" />
    <rect x="14" y="3" width="7" height="5" rx="1.5" />
    <rect x="14" y="12" width="7" height="9" rx="1.5" />
    <rect x="3" y="16" width="7" height="5" rx="1.5" />
  </Svg>
);

export const IconPerformance = (p: IconProps) => (
  <Svg {...p}>
    <path d="M3 12h4l3-8 4 16 3-8h4" />
  </Svg>
);

export const IconProcesses = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 6h13M8 12h13M8 18h13" />
    <circle cx="4" cy="6" r="1" />
    <circle cx="4" cy="12" r="1" />
    <circle cx="4" cy="18" r="1" />
  </Svg>
);

export const IconAlerts = (p: IconProps) => (
  <Svg {...p}>
    <path d="M6 16V11a6 6 0 1 1 12 0v5l2 2H4z" />
    <path d="M10 21h4" />
  </Svg>
);

export const IconSettings = (p: IconProps) => (
  <Svg {...p}>
    <path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0" />
    <circle cx="16" cy="6" r="2" />
    <circle cx="10" cy="12" r="2" />
    <circle cx="18" cy="18" r="2" />
  </Svg>
);

export const IconInfo = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 11v5M12 8h0" />
  </Svg>
);

export const IconWarning = (p: IconProps) => (
  <Svg {...p}>
    <path d="M12 3 2 20h20z" />
    <path d="M12 10v4M12 17h0" />
  </Svg>
);

export const IconCritical = (p: IconProps) => (
  <Svg {...p}>
    <path d="M8 3h8l5 5v8l-5 5H8l-5-5V8z" />
    <path d="M9 9l6 6M15 9l-6 6" />
  </Svg>
);

export const IconCheck = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="m8 12 3 3 5-6" />
  </Svg>
);

export const IconSearch = (p: IconProps) => (
  <Svg {...p}>
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.5-3.5" />
  </Svg>
);

/** Original mark: a monitor outline with a pulse line. */
export const Logo = (p: IconProps) => (
  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true" {...p}>
    <rect x="2.5" y="4" width="19" height="13" rx="2.5" stroke="currentColor" strokeWidth="1.8" />
    <path d="M6 11h3l1.5-3 3 6 1.5-3h3" stroke="var(--accent)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    <path d="M9 20.5h6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
  </svg>
);
