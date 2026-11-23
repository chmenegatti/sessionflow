import { AnimatedBackground, hoverItem } from "@/components/ui/animated-background";
import { BlurFade } from "@/components/ui/blur-fade";
import type { SessionItem } from "@/lib/api";
import { SessionRow } from "./SessionRow";

// Lista de sessões com entrada escalonada (só as primeiras linhas, para não
// atrasar listas longas) e realce que desliza até a linha sob o mouse.
export function SessionRows({ items, showProject = true }: { items: SessionItem[]; showProject?: boolean }) {
  return (
    <AnimatedBackground className="divide-y" highlightClassName="bg-white/[0.04]">
      {items.map((s, i) => (
        <div key={s.id} {...hoverItem}>
          <BlurFade className="relative" delay={i < 12 ? i * 0.03 : 0} offset={4} blur="4px">
            <SessionRow session={s} showProject={showProject} />
          </BlurFade>
        </div>
      ))}
    </AnimatedBackground>
  );
}
