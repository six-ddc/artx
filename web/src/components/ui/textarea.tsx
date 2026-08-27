import * as React from 'react';
import { cn } from '@/lib/utils';

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        'w-full resize-none rounded border border-line bg-transparent px-2.5 py-2 text-sm text-ink outline-none placeholder:text-ink-3',
        className,
      )}
      {...props}
    />
  );
}
