<div align="center" dir="rtl">
  <img src="web/static/img/favicon.svg" width="88" height="88" alt="نشان WG-Guard">
  <h1>وی‌گارد · WG-Guard</h1>
  <p><strong>پنل خودمیزبان، شیک و عملیاتی برای مدیریت نود AmneziaWG</strong></p>
  <p>یک فایل اجرایی Go · پایگاه SQLite · معماری SSR + HTMX · فارسی و انگلیسی · پشتیبانی کامل RTL · رابط REST</p>
  <p>
    زبان:
    <a href="README.fa.md"><strong>فارسی</strong></a>
    ·
    <a href="README.md">English</a>
  </p>
  <p>
    <img alt="نسخه Go" src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white">
    <img alt="نسخه اوبونتو" src="https://img.shields.io/badge/Ubuntu-24.04-E95420?logo=ubuntu&logoColor=white">
    <img alt="معماری پردازنده" src="https://img.shields.io/badge/Architecture-amd64-4F46E5">
    <img alt="مجوز" src="https://img.shields.io/badge/License-MIT-16A34A">
  </p>
</div>

> [!IMPORTANT]
> **وضعیت پروژه:** فازهای ۰ تا ۱۰ کامل هستند؛ گواهی تولید در فاز ۱۱ در جریان است و
> هنوز انتشار عمومی انجام نشده است. هدف تأییدشدهٔ فعلی، اوبونتو ۲۴٫۰۴ روی معماری amd64 است.

## چرا وی‌گارد؟

وی‌گارد نصب سریع یک پنل سبک را با ابزارهای لازم برای مدیریت حرفه‌ای یک نود VPN ترکیب می‌کند:

| نشان | قابلیت |
|---|---|
| 🛡️ | **اینترفیس‌های AmneziaWG** با پورت، شبکه، MTU و پروفایل‌های ضد DPI که روی سرور ساخته می‌شوند |
| 👥 | **مدیریت تجاری کاربران** با حجم، انقضا، شروع با نخستین اتصال، دستگاه، پلن و محدودیت مستقل سرعت |
| 📱 | **تحویل جداگانه به هر دستگاه** با فایل استاندارد .conf، کد QR، دانلود گروهی ZIP و جایگزینی امن دسترسی |
| 📊 | **داشبورد عملیاتی** برای سلامت میزبان، پردازنده، حافظه، دیسک، نرخ زندهٔ VPN، همتاها و تاریخچهٔ ترافیک |
| 🔌 | **رابط پایدار REST** با توکن سطح‌بندی‌شده، idempotency، صفحه‌بندی cursor، محدودسازی نرخ، OpenAPI و webhook امضاشده |
| 💾 | **چرخهٔ بازیابی** با پشتیبان رمزنگاری‌شده، زمان‌بندی، ارسال تلگرام، بازبینی فایل ورودی و restore مرحله‌ای |
| 🌐 | **رابط دوزبانهٔ پرمیوم** با فارسی و انگلیسی، RTL/LTR، تم روشن و تیره و system، و طراحی واکنش‌گرا |
| ⚙️ | **مدیریت امن میزبان** برای Docker یا systemd، حالت‌های HTTPS، عیب‌یابی، rollback و حذف تمیز |

معماری رابط سبک می‌ماند و از HTML رندرشده روی سرور، HTMX و JavaScript محدود استفاده می‌کند.
محیط تولید به SPA، فریم‌ورک سنگین یا Node.js نیاز ندارد.

## نصب سریع

نصب‌کننده **اوبونتو ۲۴٫۰۴ یا جدیدتر روی amd64/x86_64** را می‌پذیرد؛ تأیید واقعی تولید
فعلاً فقط برای اوبونتو ۲۴٫۰۴ انجام شده و نسخه‌های جدیدتر به آزمون مستقل نیاز دارند.
تا پیش از انتشار نسخهٔ پایدار، سورس بازبینی‌شدهٔ شاخهٔ main را نصب کنید:

~~~bash
bash -o pipefail -c 'curl --proto "=https" --proto-redir "=https" --tlsv1.2 -fsSL https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh | bash -s -- --commit main'
~~~

اسکریپت ابتدا revision انتخاب‌شده را تأیید می‌کند، مدیر محلی را نصب می‌کند و منوی راهنمای
انگلیسی را باز می‌کند. گزینهٔ **Install WG-Guard** را انتخاب کنید؛ Docker انتخاب پیشنهادی است.
دامنه را برای HTTPS خودکار وارد کنید یا برای دسترسی خصوصی با SSH tunnel خالی بگذارید.

پیش از اجرا می‌توانید فایل نصب را بررسی کنید:

~~~bash
curl --proto '=https' -fsSLo wg-guard-install.sh https://raw.githubusercontent.com/Sir-Adnan/wg-guard/main/install.sh
less wg-guard-install.sh
bash wg-guard-install.sh --commit main
~~~

## مدیریت نود

دستورهای اصلی میزبان در ادامه آمده‌اند:

~~~bash
sudo wg-guard                         # منوی مدیریت محلی
sudo wg-guard status                  # وضعیت سرویس و استقرار
sudo wg-guard doctor                  # عیب‌یابی تنظیمات و میزبان
sudo wg-guard logs                    # گزارش‌های عملیاتی اخیر
sudo wg-guard logs --follow --component awg
sudo wg-guard update                  # چرخهٔ تأییدشدهٔ پنل و هسته
~~~

مدیر محلی به‌روزرسانی، rollback، دسترسی پنل، پشتیبان‌گیری، restore و حذف را پوشش می‌دهد.
سامانه بستهٔ دلخواه AmneziaWG را نمی‌پذیرد، کلید و گذرواژه را log نمی‌کند و سرویس‌های
نامرتبط میزبان را حذف نمی‌کند.

## نمای معماری

نمای سادهٔ ارتباط اجزا در ادامه دیده می‌شود:

~~~text
مرورگر ── SSR + HTMX ── پنل Go ── سرویس‌های دامنه ── SQLite
                         │                 │
                         ├── REST /api/v1  ├── همگام‌سازی AmneziaWG
                         ├── OpenAPI       ├── nftables + shaping
                         └── webhook       └── پشتیبان + مدیر چرخهٔ عمر
~~~

- **پردازش زمینه‌ای:** کارها محدود، صف‌ها کراندار و مصرف منابع کنترل‌شده‌اند.
- **خروجی کانفیگ:** همهٔ مسیرهای مستقیم، API، پنل و اشتراک از یک renderer مشترک استفاده می‌کنند.
- **قواعد فایروال:** مالکیت WG-Guard نام‌گذاری‌شده است و قوانین سرویس‌های دیگر دست‌کاری نمی‌شوند.
- **حفاظت اسرار:** اطلاعات حساس در ذخیره‌سازی، پاسخ‌ها و گزارش‌ها محافظت می‌شوند.
- **روش استقرار:** Docker مسیر پیشنهادی است و native systemd نیز پشتیبانی می‌شود.

## مستندات

- **راهنمای نصب:** [نصب از GitHub](docs/operations/github-install.md)
- **روش استقرار:** [استقرار و HTTPS](docs/operations/deployment.md)
- **بازیابی داده:** [پشتیبان‌گیری و restore](docs/operations/backup-restore.md)
- **رابط برنامه‌نویسی:** [مستند REST API](docs/architecture/api.md)
- **سازگاری کلاینت:** [یکپارچگی AmneziaWG](docs/integrations/amneziawg.md)
- **ساختار سامانه:** [نمای معماری](docs/architecture/overview.md)
- **وضعیت پروژه:** [ماتریس توسعه](docs/development/status.md)
- **فهرست کامل:** [نقشهٔ مستندات](docs/README.md)

کانفیگ‌های مبهم‌سازی‌شده به کلاینت سازگار AmneziaWG نیاز دارند. پروفایل Standard/plain با
کلاینت معمولی WireGuard سازگار است. پیش از انتخاب پروفایل، جدول سازگاری را بررسی کنید.

## توسعه

فرمان‌های اصلی توسعه در ادامه آمده‌اند:

~~~bash
make build
make test
make lint
~~~

راهنمای [عامل‌های توسعه](AGENTS.md) و [چرخهٔ توسعه](docs/development/workflow.md) پیش از مشارکت
باید خوانده شوند.

## مجوز

پروژه با [مجوز MIT](LICENSE) منتشر می‌شود. اطلاعیه‌های وابستگی‌ها در
[فایل THIRD_PARTY](THIRD_PARTY.md) قرار دارند و اجزای AmneziaWG به‌صورت پردازش جدا اجرا می‌شوند.
