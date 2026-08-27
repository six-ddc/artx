import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

const badgeVariants = cva(
  'art-mono inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium uppercase leading-none tracking-[0.05em]',
  {
    variants: {
      variant: {
        default: 'bg-muted text-ink-2',
        open: 'bg-marker/15 text-marker-ink',
        addressed: 'bg-addressed/15 text-addressed',
        resolved: 'bg-resolved/15 text-resolved',
        orphan: 'border border-dashed border-ink-3 text-ink-3',
        outline: 'border border-line text-ink-2',
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
