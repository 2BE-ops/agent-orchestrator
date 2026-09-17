import { Cloud, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useCloudGate } from "../hooks/useCloudGate";
import { useUpdateCloudOffering } from "../hooks/useSettings";
import { Switch } from "./ui/switch";

/** Step: cloud. Optional, off by default, and the switch here is what makes the
 *  cloud project choice appear in the project step that follows. */
export function OnboardingCloudStep({ cloudEnabled }: { cloudEnabled: boolean }) {
	const { t } = useTranslation();
	const gate = useCloudGate();
	const offering = useUpdateCloudOffering();
	const enabled = gate.cloudEnabled || cloudEnabled;

	return (
		<div className="flex w-full max-w-[440px] flex-col gap-4 text-left">
			<div className="flex items-center gap-3 rounded-lg bg-card px-4 py-3">
				<span className="grid size-8 shrink-0 place-items-center text-muted-foreground [&_svg]:size-4">
					<Cloud aria-hidden="true" />
				</span>
				<span className="min-w-0 flex-1">
					<span className="block text-sm font-medium leading-5 text-foreground">{t("onboarding.cloudToggleLabel")}</span>
					<span className="mt-0.5 block text-caption leading-snug text-muted-foreground">{t("onboarding.cloudToggleDetail")}</span>
				</span>
				{offering.saving ? (
					<Loader2 aria-hidden="true" className="size-3.5 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />
				) : null}
				<Switch
					aria-label={t("onboarding.cloudToggleLabel")}
					checked={enabled}
					disabled={offering.saving}
					onCheckedChange={(next) => offering.update(next)}
				/>
			</div>
			<p className="px-1 text-center text-caption leading-snug text-muted-foreground/80">{t("onboarding.cloudCaveat")}</p>
			{offering.error ? (
				<p className="px-1 text-caption leading-snug text-destructive" role="alert">
					{offering.error}
				</p>
			) : null}
		</div>
	);
}
