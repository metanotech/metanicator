import { useState, useEffect } from "react";
import { cn } from "../../lib/utils";

export const METANICATOR_LOGO_DATA_URI =
  "data:image/png;base64,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";

interface MetanicatorIconProps extends React.ComponentProps<"span"> {
  /**
   * If true, play a one-time entrance spin animation.
   */
  animate?: boolean;
  /**
   * If true, disable hover spin animation.
   */
  noSpin?: boolean;
  /**
   * If true, show a border around the icon.
   */
  bordered?: boolean;
  /**
   * Size of the bordered icon: "sm" (default), "md", "lg"
   */
  size?: "sm" | "md" | "lg";
}

const borderedSizes = {
  sm: { wrapper: "p-1.5", icon: "size-3.5" },
  md: { wrapper: "p-2", icon: "size-4" },
  lg: { wrapper: "p-2.5", icon: "size-5" },
};

/**
 * Metanicator logo icon component.
 */
export function MetanicatorIcon({
  className,
  animate = false,
  noSpin = false,
  bordered = false,
  size = "sm",
  ...props
}: MetanicatorIconProps) {
  const [entranceDone, setEntranceDone] = useState(!animate);

  useEffect(() => {
    if (!animate) return;
    const timer = setTimeout(() => setEntranceDone(true), 600);
    return () => clearTimeout(timer);
  }, [animate]);

  if (bordered) {
    const sizeConfig = borderedSizes[size];
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center border border-border rounded-md",
          sizeConfig.wrapper,
          className
        )}
        aria-hidden="true"
        {...props}
      >
        <span
          className={cn(
            "block",
            sizeConfig.icon,
            !entranceDone && "animate-entrance-spin",
            entranceDone && !noSpin && "hover:animate-spin"
          )}
        >
          <img
            src={METANICATOR_LOGO_DATA_URI}
            alt="Metanicator logo"
            className="block size-full object-contain"
          />
        </span>
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-block size-[1em]",
        !entranceDone && "animate-entrance-spin",
        entranceDone && !noSpin && "hover:animate-spin",
        className
      )}
      aria-hidden="true"
      {...props}
    >
      <img
        src={METANICATOR_LOGO_DATA_URI}
        alt="Metanicator logo"
        className="block size-full object-contain"
      />
    </span>
  );
}
