import * as React from 'react';
import { cn } from '@/lib/utils';

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        'w-full resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm text-foreground shadow-xs transition-[color,border-color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20 disabled:opacity-50',
        className,
      )}
      {...props}
    />
  );
}
