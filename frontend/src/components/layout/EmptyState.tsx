import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { motion } from "motion/react";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
}

export function EmptyState({ icon: Icon, title, description, action }: EmptyStateProps) {
  return (
    <motion.div
      className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center"
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.25, ease: "easeOut" }}
    >
      <div className="surface flex size-11 items-center justify-center rounded-xl shadow-[0_0_30px_-8px_rgba(56,225,245,0.45)]">
        <Icon className="size-5 text-primary/80" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-medium">{title}</p>
        {description && <p className="max-w-sm text-sm text-muted-foreground">{description}</p>}
      </div>
      {action}
    </motion.div>
  );
}
