import { useId } from "react";
import { cn } from "@/lib/utils";
import { brand } from "@/lib/branding.generated";

type SiftLogoProps = {
  className?: string;
  title?: string;
};

/** Envelope over a sieve. currentColor → --foreground from branding.yaml. */
export function SiftLogo({ className, title = brand.name }: SiftLogoProps) {
  const uid = useId().replace(/:/g, "");
  const bowlMask = `sift-bowl-${uid}`;

  return (
    <svg
      viewBox="0 0 128 100"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label={title}
      className={cn("shrink-0 text-foreground", className)}
    >
      <title>{title}</title>
      <defs>
        <mask id={bowlMask} maskUnits="userSpaceOnUse">
          <path d="M30 58 A34 34 0 0 0 98 58 Z" fill="#fff" />
          <circle cx="48" cy="70" r="2.4" fill="#000" />
          <circle cx="58" cy="68" r="1.8" fill="#000" />
          <circle cx="68" cy="71" r="2.6" fill="#000" />
          <circle cx="78" cy="69" r="2" fill="#000" />
          <circle cx="54" cy="78" r="2.2" fill="#000" />
          <circle cx="64" cy="80" r="1.7" fill="#000" />
          <circle cx="74" cy="77" r="2.4" fill="#000" />
          <circle cx="44" cy="82" r="1.6" fill="#000" />
          <circle cx="84" cy="78" r="1.9" fill="#000" />
          <circle cx="59" cy="88" r="2" fill="#000" />
          <circle cx="70" cy="87" r="1.8" fill="#000" />
          <circle cx="50" cy="90" r="1.5" fill="#000" />
          <circle cx="79" cy="86" r="1.6" fill="#000" />
          <circle cx="64" cy="74" r="1.4" fill="#000" />
          <circle cx="72" cy="73" r="1.5" fill="#000" />
          <circle cx="56" cy="73" r="1.3" fill="#000" />
          <circle cx="46" cy="76" r="1.4" fill="#000" />
          <circle cx="82" cy="73" r="1.5" fill="#000" />
        </mask>
      </defs>

      <rect
        x="36"
        y="6"
        width="56"
        height="46"
        rx="2.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="5"
      />
      <path
        d="M38 12 L64 34 L90 12"
        fill="none"
        stroke="currentColor"
        strokeWidth="5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />

      <line
        x1="6"
        y1="58"
        x2="18"
        y2="58"
        stroke="currentColor"
        strokeWidth="5"
        strokeLinecap="round"
      />
      <line
        x1="26"
        y1="58"
        x2="102"
        y2="58"
        stroke="currentColor"
        strokeWidth="5"
        strokeLinecap="round"
      />
      <line
        x1="110"
        y1="58"
        x2="122"
        y2="58"
        stroke="currentColor"
        strokeWidth="5"
        strokeLinecap="round"
      />

      <path d="M30 58 A34 34 0 0 0 98 58 Z" fill="currentColor" mask={`url(#${bowlMask})`} />
    </svg>
  );
}

export function SiftWordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <SiftLogo className="size-8" />
      <span className="font-display tracking-tight">{brand.name}</span>
    </span>
  );
}
