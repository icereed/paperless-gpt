import classNames from "classnames";
import React from "react";

type Variant = "primary" | "secondary" | "danger" | "ghost";
type Size = "md" | "sm";

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
}

const variantClasses: Record<Variant, string> = {
  primary: "bg-primary text-primary-ink hover:bg-primary-hover",
  secondary: "border border-line bg-surface text-ink hover:bg-surface-2",
  danger: "border border-neg text-neg hover:bg-neg-tint",
  ghost: "text-muted hover:bg-surface-2 hover:text-ink",
};

const sizeClasses: Record<Size, string> = {
  md: "h-9 px-4 text-sm",
  sm: "h-8 px-3 text-sm",
};

/** The app's single button vocabulary: primary / secondary / danger / ghost. */
const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { variant = "secondary", size = "md", loading, disabled, className, children, ...rest },
    ref
  ) => (
    <button
      ref={ref}
      disabled={disabled || loading}
      className={classNames(
        "inline-flex select-none items-center justify-center gap-2 rounded-md font-medium",
        "transition-colors duration-150 ease-out-quart",
        "disabled:pointer-events-none disabled:opacity-50",
        variantClasses[variant],
        sizeClasses[size],
        className
      )}
      {...rest}
    >
      {loading && (
        <svg
          className="h-4 w-4 animate-spin"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
        >
          <circle
            className="opacity-25"
            cx="12"
            cy="12"
            r="10"
            stroke="currentColor"
            strokeWidth="4"
          />
          <path
            className="opacity-90"
            fill="currentColor"
            d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z"
          />
        </svg>
      )}
      {children}
    </button>
  )
);

Button.displayName = "Button";

export default Button;
