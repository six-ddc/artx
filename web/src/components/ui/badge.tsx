import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

/* Status is carried Vercel-style: a small colored dot next to neutral text
   (never a color-filled pill, never a stripe). The dot is a ::before pseudo
   so callers keep passing plain text children. */
const badgeVariants = cva(
  'inline-flex items-center gap-1.5 whitespace-nowrap text-xs font-medium leading-none',
  {
    variants: {
      variant: {
        default: 'rounded-full bg-muted px-2 py-1 text-muted-foreground',
        outline: 'rounded-full border px-2 py-1 text-muted-foreground',
        open: "text-foreground before:size-2 before:shrink-0 before:rounded-full before:bg-status-open before:content-['']",
        addressed:
          "text-foreground before:size-2 before:shrink-0 before:rounded-full before:bg-status-addressed before:content-['']",
        resolved:
          "text-foreground before:size-2 before:shrink-0 before:rounded-full before:bg-status-resolved before:content-['']",
        orphan:
          "text-muted-foreground before:size-2 before:shrink-0 before:rounded-full before:border before:border-dashed before:border-muted-foreground before:content-['']",
      },
    },
    defaultVariants: { variant: 'default' },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, className }))} {...props} />;
}
