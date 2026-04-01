import type { FC } from 'hono/jsx'
import { Icon } from './icon'

export const FeatureCard: FC<{ icon: string; title: string; description: string; href: string }> = ({ icon, title, description, href }) => (
  <a href={href} class="group rounded-lg border border-deep-purple/30 bg-surface/50 p-5 transition-all duration-300 glow-box-hover hover:border-deep-purple/60 block no-underline">
    <div class="text-lavender mb-3">
      <Icon name={icon} class="w-7 h-7" />
    </div>
    <h3 class="font-mono font-semibold text-pink text-sm mb-2">{title}</h3>
    <p class="text-muted text-sm leading-relaxed">{description}</p>
  </a>
)
