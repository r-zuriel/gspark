# Monitoring basics

A monitoring setup should cover metrics (numbers over time), logs (discrete events) and alerts
(a human is notified when something crosses a threshold). Start from the user-facing symptoms,
not the internals: latency, errors, saturation and traffic.

Good alerts are actionable and rare; noisy alerts train people to ignore them. Every alert
should link to a runbook that says what to check and how to recover.
