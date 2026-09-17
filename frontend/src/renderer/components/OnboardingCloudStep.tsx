import { Cloud, Laptop } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCloudGate } from "../hooks/useCloudGate";
import { useUpdateCloudOffering } from "../hooks/useSettings";
import { SetupRow } from "./SetupList";

/** Step: cloud. Cloud is additive, so the options read as "add it" or "leave it
 *  alone" rather than a mode switch. Choosing marks the row and tells the
 *  daemon in the background; Continue moves the flow on. */
export function OnboardingCloudStep({ cloudEnabled }: { cloudEnabled: boolean }) {
	const { t } = useTranslation();
	const gate = useCloudGate();
	const offering = useUpdateCloudOffering();
	const enabled = gate.cloudEnabled || cloudEnabled;
	const [choice, setChoice] = useState<boolean | null>(null);
	const selected = choice ?? enabled;

	const choose = (next: boolean) => {
		setChoice(next);
		offering.update(next);
	};

	return (
		<div className="flex w-full max-w-[440px] flex-col gap-4 text-left">
			<div className="flex w-full flex-col gap-3">
				<SetupRow
					variant="card"
					icon={<Cloud aria-hidden="true" />}
					label={t("onboarding.cloudOptionYesLabel")}
					description={t("onboarding.cloudOptionYesDetail")}
					selected={selected}
					onClick={() => choose(true)}
				/>
				<SetupRow
					variant="card"
					icon={<Laptop aria-hidden="true" />}
					label={t("onboarding.cloudOptionNoLabel")}
					description={t("onboarding.cloudOptionNoDetail")}
					selected={!selected}
					onClick={() => choose(false)}
				/>
			</div>
			<p className="px-1 text-center text-caption leading-snug text-muted-foreground/80">{t("onboarding.cloudCaveat")}</p>
			{offering.error ? (
				<p className="px-1 text-center text-caption leading-snug text-destructive" role="alert">
					{offering.error}
				</p>
			) : null}
		</div>
	);
}
