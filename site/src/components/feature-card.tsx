import type { FC } from 'hono/jsx'

export const FeatureCard: FC<{ icon: string; title: string; description: string }> = ({ icon, title, description }) => (
  <div class="group rounded-lg border border-deep-purple/30 bg-surface/50 p-5 transition-all duration-300 glow-box-hover hover:border-deep-purple/60">
    <div class="text-2xl mb-3">{icon}</div>
    <h3 class="font-mono font-semibold text-pink text-sm mb-2">{title}</h3>
    <p class="text-muted text-sm leading-relaxed">{description}</p>
  </div>
)
