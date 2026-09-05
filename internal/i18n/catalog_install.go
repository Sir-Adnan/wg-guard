package i18n

// Terminal prerequisite, core and TLS messages are shared with the M4 terminal UI.
func init() {
	catalogEN["install.error.manual_pending"] = "TLS certificate readiness pending; check the trusted chain, certificate domain/IP and configured files, then retry with wg-guard tls-check: %v"
	catalogFA["install.error.manual_pending"] = "آمادگی گواهی TLS در انتظار است؛ زنجیرهٔ مورد اعتماد، دامنه/IP گواهی و فایل‌های تنظیم‌شده را بررسی و با wg-guard tls-check دوباره تلاش کنید: %v"
	catalogEN["install.cli.public_ip"] = "public VPN endpoint IP (required behind NAT)"
	catalogFA["install.cli.public_ip"] = "IP عمومی VPN (پشت NAT الزامی است)"
	catalogEN["install.cli.prerequisites"] = "auto (Ubuntu 24.04) | check (manual prerequisites)"
	catalogFA["install.cli.prerequisites"] = "auto (Ubuntu 24.04) | check (پیش‌نیازهای دستی)"
	catalogEN["install.cli.core"] = "recommended | latest-compatible | exact compatible bundle ID"
	catalogFA["install.cli.core"] = "recommended | latest-compatible | شناسهٔ دقیق بستهٔ سازگار"
	catalogEN["install.cli.skip_module"] = "explicit external/manual host module management; AWG tools are still checked"
	catalogFA["install.cli.skip_module"] = "مدیریت صریح خارجی/دستی ماژول میزبان؛ ابزار AWG همچنان بررسی می‌شود"
	catalogEN["install.cli.arguments"] = "install: unexpected positional arguments"
	catalogFA["install.cli.arguments"] = "نصب: آرگومان موقعیتی غیرمنتظره"
	catalogEN["install.cli.policy"] = "install: prerequisites must be auto or check"
	catalogFA["install.cli.policy"] = "نصب: پیش‌نیازها باید auto یا check باشند"
	catalogEN["install.cli.core_usage"] = "core: use installed | recommended | latest-compatible | exact BUNDLE"
	catalogFA["install.cli.core_usage"] = "core: از installed | recommended | latest-compatible | exact BUNDLE استفاده کنید"
	catalogEN["install.cli.core_exact"] = "core: exact requires one catalog bundle ID"
	catalogFA["install.cli.core_exact"] = "core: گزینهٔ exact به یک شناسهٔ بسته از فهرست نیاز دارد"
	catalogEN["install.cli.core_arguments"] = "core: unexpected arguments"
	catalogFA["install.cli.core_arguments"] = "core: آرگومان‌های غیرمنتظره"
	catalogEN["install.cli.tls_arguments"] = "tls-check: no arguments expected"
	catalogFA["install.cli.tls_arguments"] = "tls-check: آرگومانی پذیرفته نمی‌شود"
	catalogEN["install.cli.help"] = "\nPrerequisites and certificates:\n  install     --public-ip IP --prerequisites auto|check --core BUNDLE\n  core        installed | recommended | latest-compatible | exact BUNDLE\n  tls-check   Retry certificate verification for an installed node\n"
	catalogFA["install.cli.help"] = "\nپیش‌نیازها و گواهی‌ها:\n  install     --public-ip IP --prerequisites auto|check --core BUNDLE\n  core        installed | recommended | latest-compatible | exact BUNDLE\n  tls-check   تلاش مجدد برای تأیید گواهی گره نصب‌شده\n"
	catalogEN["install.error.systemd"] = "install: native mode requires systemd as PID 1"
	catalogFA["install.error.systemd"] = "نصب: حالت native به systemd با PID 1 نیاز دارد"
	catalogEN["install.summary.certificate"] = "   2. Certificate readiness: %s (HTTP-01 needs external TCP 80).\n"
	catalogFA["install.summary.certificate"] = "   2. آمادگی گواهی: %s (HTTP-01 به TCP 80 خارجی نیاز دارد).\n"
	catalogEN["install.summary.core"] = "  Core requested: %s (tools %s; kernel source %s)\n  Core installed: tools %s; kernel package %s\n  Module loaded:  %s; identity %s; reboot required %t\n"
	catalogFA["install.summary.core"] = "  هستهٔ درخواستی: %s (ابزار %s؛ منبع کرنل %s)\n  هستهٔ نصب‌شده: ابزار %s؛ بستهٔ کرنل %s\n  ماژول بارگذاری‌شده: %s؛ هویت %s؛ نیاز به راه‌اندازی مجدد %t\n"
	catalogEN["install.error.manual_path"] = "install: manual certificate paths must be absolute and contain no whitespace or compose delimiters"
	catalogFA["install.error.manual_path"] = "نصب: مسیر گواهی دستی باید مطلق و بدون فاصله یا جداکننده‌های Compose باشد"
	catalogEN["install.error.manual_files"] = "install: manual certificate and private key files must be readable before installation"
	catalogFA["install.error.manual_files"] = "نصب: فایل‌های گواهی دستی و کلید خصوصی باید پیش از نصب قابل خواندن باشند"
	catalogEN["install.error.manual_pair"] = "install: manual certificate and private key are not a valid matching pair"
	catalogFA["install.error.manual_pair"] = "نصب: گواهی دستی و کلید خصوصی یک جفت معتبر و مطابق نیستند"
	for key, value := range map[string]string{
		"install.error.core.1":     "install: unknown compatible core bundle; use recommended, latest-compatible or awg-2026-08",
		"install.error.core.2":     "install: prerequisite policy must be auto or check",
		"install.error.core.3":     "install: core must match a catalogued bundle",
		"install.error.core.4":     "install: installed %s differs from selected bundle; preserve it and resolve compatibility manually",
		"install.error.core.5":     "install: missing %s; provision prerequisites manually (automatic setup is Ubuntu 24.04 only)",
		"install.error.core.6":     "install: existing Docker lacks Compose; install its matching Compose plugin manually",
		"install.error.core.7":     "install: refresh Ubuntu package metadata failed",
		"install.error.core.8":     "install: exact compatible AWG tools/kernel packages are unavailable after metadata refresh; no core or deployment was installed",
		"install.error.core.9":     "install: required package %s version %s is unavailable; configure the documented Ubuntu 24.04 repositories and refresh indexes",
		"install.error.core.10":    "install: prerequisite package installation failed: %v",
		"install.error.core.11":    "install: missing prerequisite %s; repair its package manually",
		"install.error.core.12":    "install: Docker Compose is unavailable",
		"install.error.core.13":    "install: Docker daemon is unavailable; start Docker and retry",
		"install.error.core.14":    "install: installed AWG tools do not match selected bundle",
		"install.error.core.15":    "install: installed kernel package does not match selected bundle",
		"install.error.core.16":    "install: loaded module differs from disk; schedule a maintenance reboot, then retry (active tunnels preserved)",
		"install.error.core.17":    "install: load the compatible AmneziaWG module manually, then retry",
		"install.error.core.18":    "install: selected module build failed; check matching headers and Secure Boot, then retry",
		"install.error.core.19":    "install: module dependency refresh failed",
		"install.error.core.20":    "install: module cannot load; check headers and Secure Boot or select explicit external core",
		"install.error.core.21":    "install: module load was not observable; managed tunnel readiness is unverified",
		"install.error.core.22":    "install: loaded module build identity is unknown; inspect loaded and disk srcversion or select explicit external core",
		"install.error.core.23":    "install: refresh Ubuntu package metadata failed",
		"install.error.core.24":    "install: Ubuntu repository tooling installation failed",
		"install.error.core.25":    "install: prepare Amnezia PPA for Ubuntu 24.04 failed",
		"install.error.core.26":    "install: refresh Amnezia PPA metadata failed",
		"install.error.platform.1": "install: Linux is required",
		"install.error.platform.2": "install: cannot inspect /etc/os-release",
		"install.error.platform.3": "install: OS identity is incomplete; provision prerequisites manually",
		"install.error.platform.4": "install: cannot inspect architecture",
		"install.error.platform.5": "install: supported architectures are amd64 and arm64",
		"install.error.platform.6": "install: cannot inspect running kernel",
		"install.error.platform.7": "install: no public VPN endpoint detected; supply --public-ip (required behind NAT) or --domain",
		"install.error.image.1":    "install: runtime image requires a catalogued core bundle",
		"install.error.image.2":    "install: runtime image requires absolute staging/artifact paths and verified build identity",
		"install.error.image.3":    "install: runtime staging parent must already exist",
		"install.error.image.4":    "install: runtime candidate is not a regular file",
		"install.error.image.5":    "install: acquired binary integrity check failed before runtime image build",
		"install.error.image.6":    "install: runtime image build failed: %v",
		"install.error.image.7":    "install: runtime image did not record immutable identity",
		"install.error.image.8":    "install: Docker returned an invalid immutable image identity",
		"install.error.health.1":   "TLS certificate readiness pending; check DNS, inbound TCP 80 forwarding to challenge port %d, CA reachability and certificate names; retry with wg-guard tls-check: %v",
		"install.error.health.2":   "TLS certificate verification requires a domain or public IP",
		"install.error.health.3":   "install: no installation state",
		"install.error.health.4":   "install: this node does not terminate TLS",
		"install.error.health.5":   "healthz answered %d",
		"install.error.health.6":   "parse %s: %v (remove it if this host was reinstalled)",
		"install.error.health.7":   "read boot config: %v",
		"install.error.health.8":   "parse boot config: %v",
		"install.error.plan.1":     "mode %q is not docker|native",
		"install.error.plan.2":     "domain must be a bare hostname with valid DNS labels",
		"install.error.plan.3":     "public-ip must be a unicast address outside private, shared and non-public special-use ranges; address classification does not verify reachability",
		"install.error.plan.4":     "Telegram chat ID must be a nonzero signed integer",
		"install.error.plan.5":     "tls.mode=acme requires a domain",
		"install.error.plan.6":     "domain %q must be a bare hostname",
		"install.error.plan.7":     "tls.mode=manual requires --cert-file and --key-file",
		"install.error.plan.8":     "tls mode %q is not acme|manual|proxy|dev",
		"install.error.plan.9":     "panel port %d is out of range 1-65535",
		"install.error.plan.10":    "acme http port %d is out of range 1-65535",
		"install.error.plan.11":    "panel and ACME challenge TCP ports must differ",
	} {
		catalogEN[key] = value
	}
	for key, value := range map[string]string{
		"install.error.core.1":     "نصب: بستهٔ سازگار ناشناخته است؛ از recommended، latest-compatible یا awg-2026-08 استفاده کنید",
		"install.error.core.2":     "نصب: روش پیش‌نیازها باید auto یا check باشد",
		"install.error.core.3":     "نصب: هسته باید با یکی از بسته‌های فهرست سازگار مطابقت داشته باشد",
		"install.error.core.4":     "نصب: نسخهٔ نصب‌شدهٔ %s با بستهٔ انتخابی متفاوت است؛ آن را حفظ و سازگاری را دستی بررسی کنید",
		"install.error.core.5":     "نصب: %s موجود نیست؛ پیش‌نیازها را دستی آماده کنید (نصب خودکار فقط برای Ubuntu 24.04 است)",
		"install.error.core.6":     "نصب: Docker موجود افزونهٔ Compose ندارد؛ افزونهٔ سازگار با آن را دستی نصب کنید",
		"install.error.core.7":     "نصب: به‌روزرسانی فهرست بسته‌های Ubuntu ناموفق بود",
		"install.error.core.8":     "نصب: نسخه‌های دقیق ابزار و ماژول AWG پس از به‌روزرسانی فهرست موجود نیستند؛ هسته یا سرویس نصب نشد",
		"install.error.core.9":     "نصب: بستهٔ %s با نسخهٔ %s موجود نیست؛ مخازن مستند Ubuntu 24.04 را تنظیم و فهرست را به‌روز کنید",
		"install.error.core.10":    "نصب: نصب بسته‌های پیش‌نیاز ناموفق بود: %v",
		"install.error.core.11":    "نصب: پیش‌نیاز %s موجود نیست؛ بستهٔ آن را دستی ترمیم کنید",
		"install.error.core.12":    "نصب: Docker Compose در دسترس نیست",
		"install.error.core.13":    "نصب: سرویس Docker در دسترس نیست؛ آن را راه‌اندازی و دوباره تلاش کنید",
		"install.error.core.14":    "نصب: ابزار AWG نصب‌شده با بستهٔ انتخابی مطابقت ندارد",
		"install.error.core.15":    "نصب: بستهٔ ماژول نصب‌شده با بستهٔ انتخابی مطابقت ندارد",
		"install.error.core.16":    "نصب: ماژول بارگذاری‌شده با نسخهٔ روی دیسک متفاوت است؛ راه‌اندازی مجدد را در زمان نگهداری انجام دهید و دوباره تلاش کنید (تونل‌های فعال حفظ شدند)",
		"install.error.core.17":    "نصب: ماژول سازگار AmneziaWG را دستی بارگذاری و دوباره تلاش کنید",
		"install.error.core.18":    "نصب: ساخت ماژول انتخابی ناموفق بود؛ هدرهای کرنل و Secure Boot را بررسی کنید",
		"install.error.core.19":    "نصب: به‌روزرسانی وابستگی‌های ماژول ناموفق بود",
		"install.error.core.20":    "نصب: ماژول بارگذاری نمی‌شود؛ هدرها و Secure Boot را بررسی یا مدیریت خارجی هسته را صریحاً انتخاب کنید",
		"install.error.core.21":    "نصب: بارگذاری ماژول مشاهده نشد؛ آمادگی تونل‌های مدیریت‌شده تأیید نشده است",
		"install.error.core.22":    "نصب: هویت ساخت ماژول بارگذاری‌شده نامشخص است؛ srcversion حافظه و دیسک را بررسی یا مدیریت خارجی هسته را انتخاب کنید",
		"install.error.core.23":    "نصب: به‌روزرسانی فهرست بسته‌های Ubuntu ناموفق بود",
		"install.error.core.24":    "نصب: نصب ابزار مخزن Ubuntu ناموفق بود",
		"install.error.core.25":    "نصب: آماده‌سازی مخزن Amnezia PPA برای Ubuntu 24.04 ناموفق بود",
		"install.error.core.26":    "نصب: به‌روزرسانی فهرست Amnezia PPA ناموفق بود",
		"install.error.platform.1": "نصب: Linux لازم است",
		"install.error.platform.2": "نصب: بررسی /etc/os-release ممکن نیست",
		"install.error.platform.3": "نصب: هویت سیستم‌عامل ناقص است؛ پیش‌نیازها را دستی آماده کنید",
		"install.error.platform.4": "نصب: بررسی معماری ممکن نیست",
		"install.error.platform.5": "نصب: معماری‌های پشتیبانی‌شده amd64 و arm64 هستند",
		"install.error.platform.6": "نصب: بررسی کرنل در حال اجرا ممکن نیست",
		"install.error.platform.7": "نصب: نشانی عمومی VPN پیدا نشد؛ --public-ip (پشت NAT الزامی است) یا --domain را مشخص کنید",
		"install.error.image.1":    "نصب: ساخت ایمیج به یک بستهٔ هسته از فهرست سازگار نیاز دارد",
		"install.error.image.2":    "نصب: ساخت ایمیج به مسیرهای مطلق آماده‌سازی و فایل و هویت ساخت تأییدشده نیاز دارد",
		"install.error.image.3":    "نصب: پوشهٔ والد آماده‌سازی ایمیج باید از قبل موجود باشد",
		"install.error.image.4":    "نصب: فایل برنامهٔ انتخابی یک فایل عادی نیست",
		"install.error.image.5":    "نصب: بررسی یکپارچگی فایل دریافت‌شده پیش از ساخت ایمیج ناموفق بود",
		"install.error.image.6":    "نصب: ساخت ایمیج اجرا ناموفق بود: %v",
		"install.error.image.7":    "نصب: ساخت ایمیج شناسهٔ تغییرناپذیر را ثبت نکرد",
		"install.error.image.8":    "نصب: Docker شناسهٔ تغییرناپذیر نامعتبر برگرداند",
		"install.error.health.1":   "آمادگی گواهی TLS در انتظار است؛ DNS، هدایت TCP 80 ورودی به پورت چالش %d، دسترسی به مرجع صدور و نام گواهی را بررسی کنید؛ با wg-guard tls-check دوباره تلاش کنید: %v",
		"install.error.health.2":   "بررسی گواهی TLS به دامنه یا IP عمومی نیاز دارد",
		"install.error.health.3":   "نصب: وضعیت نصب موجود نیست",
		"install.error.health.4":   "نصب: این گره اتصال TLS را مستقیماً دریافت نمی‌کند",
		"install.error.health.5":   "پاسخ healthz برابر %d بود",
		"install.error.health.6":   "خواندن %s: %v (اگر میزبان دوباره نصب شده است آن را حذف کنید)",
		"install.error.health.7":   "خواندن تنظیمات راه‌اندازی: %v",
		"install.error.health.8":   "تحلیل تنظیمات راه‌اندازی: %v",
		"install.error.plan.1":     "حالت %q باید docker یا native باشد",
		"install.error.plan.2":     "دامنه باید نام میزبان بدون پورت و با برچسب‌های معتبر DNS باشد",
		"install.error.plan.3":     "public-ip باید نشانی تک‌پخشی خارج از محدوده‌های خصوصی، اشتراکی و خاص غیرعمومی باشد؛ طبقه‌بندی نشانی، دسترسی‌پذیری را تأیید نمی‌کند",
		"install.error.plan.4":     "شناسهٔ گفت‌وگوی Telegram باید عدد صحیح علامت‌دار و غیرصفر باشد",
		"install.error.plan.5":     "tls.mode=acme به دامنه نیاز دارد",
		"install.error.plan.6":     "دامنهٔ %q باید نام میزبان بدون پورت باشد",
		"install.error.plan.7":     "tls.mode=manual به --cert-file و --key-file نیاز دارد",
		"install.error.plan.8":     "حالت TLS برابر %q باید acme، manual، proxy یا dev باشد",
		"install.error.plan.9":     "پورت پنل %d خارج از بازهٔ 1 تا 65535 است",
		"install.error.plan.10":    "پورت HTTP چالش ACME برابر %d خارج از بازهٔ 1 تا 65535 است",
		"install.error.plan.11":    "پورت‌های TCP پنل و چالش ACME باید متفاوت باشند",
	} {
		catalogFA[key] = value
	}
}
