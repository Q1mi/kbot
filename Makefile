.PHONY: verify verify-course verify-final

verify: verify-course verify-final

verify-course:
	$(MAKE) -C kbot-course verify

verify-final:
	$(MAKE) -C kbot-final build
	$(MAKE) -C kbot-final test
	cd kbot-final/web/admin && npm ci && npm test && npm run build
