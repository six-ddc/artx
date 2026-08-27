import * as React from 'react';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/lib/utils';

/** Native <select> styled as a mono "sha chip"; density-first, doesn't need Radix Select's overlay overhead. */
export function Select({ className, children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative inline-flex">
      <select
        className={cn(
          'art-mono h-7 appearance-none rounded border border-line bg-transparent pl-2 pr-6 text-xs text-ink outline-none',
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-1.5 top-1/2 size-3 -translate-y-1/2 text-ink-3" />
    </div>
  );
}
