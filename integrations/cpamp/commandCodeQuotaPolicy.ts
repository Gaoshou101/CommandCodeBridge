import type { CommandCodeQuotaState } from './commandCodeQuotaStore';

const planLabels = { go: 'Go', 'go-v1': 'Go', goat: 'GOAT', pro: 'Pro', 'pro-v1': 'Pro', max: 'Max', ultra: 'Max' } as const;
// Official pricing-limits and command-code 1.81.0, checked 2026-10-13.
// The original Pro ID retains 30 credits; today's 80-credit Pro is pro-v1.
// Max's standard and premium limits are separate, never added together.
const monthlyCaps: Readonly<Record<string, number>> = {
  'individual-go': 10, 'individual-go-v1': 10, 'individual-goat': 70,
  'individual-pro': 30, 'individual-pro-v1': 80,
  'individual-max': 150, 'individual-ultra': 300,
  go: 10, goat: 70, pro: 80,
};
const planKey = (plan: string | null | undefined) => plan?.trim().toLowerCase().replace(/_/g, '-');
export function formatCommandCodePlan(plan: string | null | undefined): string | null {
  const key = planKey(plan)?.replace(/^individual-/, '');
  return key && Object.prototype.hasOwnProperty.call(planLabels, key) ? planLabels[key as keyof typeof planLabels] : null;
}

export function getCommandCodeMonthlyQuota(state: CommandCodeQuotaState) {
  const bucket = state.data?.groups?.flatMap(group => group.buckets ?? []).find(item => item.window === 'monthly');
  const fraction = bucket?.remainingFraction;
  const credits = state.data?.summary?.find(metric => metric.key === 'monthly_credits')?.value;
  const remainingCredits = typeof credits === 'number' && Number.isFinite(credits) && credits >= 0 ? credits : null;
  const nativeCap = state.usage?.unavailable?.includes('credits') || state.usage?.status === 'error'
    ? undefined : state.usage?.windows?.find(window => window.name === 'monthly')?.cap;
  const plan = planKey(state.data?.subscription?.plan);
  const capCredits = typeof nativeCap === 'number' && Number.isFinite(nativeCap) && nativeCap > 0
    ? nativeCap : plan && Object.prototype.hasOwnProperty.call(monthlyCaps, plan) ? monthlyCaps[plan] : null;
  const remainingPercent = typeof fraction === 'number' && Number.isFinite(fraction)
    ? Math.max(0, Math.min(1, fraction)) * 100
    : remainingCredits !== null && capCredits !== null ? Math.max(0, Math.min(100, remainingCredits / capCredits * 100)) : null;
  return { remainingPercent, remainingCredits, capCredits };
}
