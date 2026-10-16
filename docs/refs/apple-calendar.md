# Seeing the app's Google calendar in Apple Calendar (iPhone, iPad, Mac)

Sources: official Apple and Google support pages, fetched 2026-10-16. "Updated" is the date shown on the page
("no date" if none; Google and Apple guide pages show only a 2026 copyright). Russian labels are from the `ru-ru` / `hl=ru` pages.

## Sources
| Page | Updated |
|---|---|
| G1 [Как добавить мероприятия из Google Календаря в Календарь Apple – Компьютер](https://support.google.com/calendar/answer/99358?hl=ru) | no date |
| G2 [Same article – iPhone и iPad](https://support.google.com/calendar/answer/99358?hl=ru&co=GENIE.Platform%3DiOS) | no date |
| G3 [Как синхронизировать календарь с программами на компьютере](https://support.google.com/calendar/answer/37648?hl=ru) | no date |
| G4 [Как создавать общедоступные календари и управлять ими](https://support.google.com/calendar/answer/37083?hl=ru) | no date |
| G5 [Как создать календарь](https://support.google.com/calendar/answer/37095?hl=ru) | no date |
| A1 [Добавление учетной записи электронной почты на iPhone или iPad](https://support.apple.com/ru-ru/102619) | published 2026-05-12 (en page: 2026-05-07) |
| A2 [Создание учетных записей электронной почты, контактов и календаря на iPhone](https://support.apple.com/ru-ru/guide/iphone/ipha0d932e96/ios) (iOS 27 guide) | no date; body not readable, see "Not verified" |
| A3 [Добавление календарей и учетных записей календаря на Mac](https://support.apple.com/ru-ru/guide/calendar/icl4308d6701/mac) (macOS 27) | no date |
| A4 [Использование учетных записей интернета на Mac](https://support.apple.com/ru-ru/guide/mac-help/mh35565/mac) (macOS 27) | no date |
| A5 [Обновление календарей на Mac](https://support.apple.com/ru-ru/guide/calendar/icl1024/mac) | no date |
| A6 [Изменение настроек раздела «Учетные записи» в Календаре на Mac](https://support.apple.com/ru-ru/guide/calendar/icl26669/mac) | no date |
| A7 [Подписка на календари на Mac](https://support.apple.com/ru-ru/guide/calendar/icl1022/16.0/mac/26) (macOS 26 guide) | no date |
| A8 [Если не видны события в приложении «Календарь» на Mac](https://support.apple.com/ru-ru/guide/calendar/icl1009/mac) | no date |
| A9 [Если не удается изменить календарь или событие на Mac](https://support.apple.com/ru-ru/guide/calendar/icl1033/mac) | no date |

## 1. iPhone / iPad: add the Google account (option A)
- Google's iOS steps (G2): «Настройки» > «Календарь» > «Учетные записи» > «Google», enter email and password. Mail, contacts and
  calendar sync; "if you only need Calendar, turn off the other services". For an already added account turn on «Календари».
- This is the pre-iOS 18 layout. Since iOS 18 app settings are under «Приложения»: Apple's current Mail page (A1) gives
  «Настройки» > «Приложения» > «Почта» > «Учетные записи Почты» > «Добавить учетную запись». The Calendar equivalent
  («Настройки» > «Приложения» > «Календарь» > «Учетные записи Календаря») is **not verified** (A2 body not readable).
- Two-step verification: on an older OS with 2-step on, "use an app password instead of the normal password" (G2).
- Not supported in Apple Calendar: event email notifications, creating new Google calendars, room booking (G1, G2).

## 2. Mac: add the Google account (option A)
- macOS 27, Calendar app: «Календарь» > «Добавить учетную запись» > choose the provider > «Продолжить» > follow the prompts (A3).
- macOS 27, system-wide: «Системные настройки» > «Учетные записи интернета» > «Добавить учетную запись» > Google; then pick which
  apps use the account (A4). Accounts added in Calendar also appear there (A4). Exact toggle label «Календари»: not verified.
- Google's Mac steps (G1): «Календарь» > «Настройки» > «Учетные записи» > «Добавить» > «Google» > «Продолжить»; set the sync
  frequency in «Обновить календари». Matches the older macOS layout; on macOS 27 adding is under «Календарь» menu (A3).
- Show a calendar: its checkbox in the calendar list must be on (A8); if the list is hidden: «Вид» > «Показать список календарей» (A3).
- Google: if Apple Calendar «Делегирование» was used before, turn it off («Учетные записи» > «Делегирование», uncheck all) (G1).

## 3. Which Google calendars appear on Apple devices
- By default all calendars under «Мои календари» in Google Calendar web, plus birthdays, sync (G1).
- Others: open `https://www.google.com/calendar/syncselect`, check the calendars, «Сохранить», refresh Apple Calendar (G1).
  The URL redirects to calendar.google.com/calendar/syncselect, then to Google sign-in (checked 2026-10-16).
- The creator of a calendar becomes its owner (G5). Whether an API-created (`calendar.app.created`) calendar is listed under
  «Мои календари» is not stated; the help screen should mention syncselect as the fix.

## 4. Sync timing and refresh
- Mac, account: «Календарь» > «Настройки» > «Учетные записи» > account > «Обновить календари» pop-up; with Push changes arrive
  automatically (A5, A6). Manual: «Вид» > «Обновить календари» (A5). Intervals are not listed.
- Mac, subscription: Control-click the calendar > «Свойства» > «Автообновление» (A5); also chosen when subscribing (A7).
- iPhone fetch/push settings: not verified.
- Google gives **no** refresh time for the secret iCal address (G3, checked in ru and en). The only delay found: closing public
  access may take up to 4 hours (G4).

## 5. Option B: secret iCal address (read-only fallback)
- Where (G3, web only): Google Calendar on a computer > settings icon > «Настройки» > under «Настройки моих календарей» click the
  calendar > «Интеграция календаря» > copy «Закрытый адрес в формате iCal». English UI: "Secret address in iCal format".
  The Russian label is «Закрытый…», not «Секретный…».
- Warning (G3): «Закрытый адрес календаря предназначен для личного использования. Никому не сообщайте его.»
- Reset (G3): if someone learned it, press «Сбросить»; the old address stops working.
- May be missing (G3): for work/school accounts the admin may have changed the settings; then contact the admin.
- Sign-in sync = view and edit; iCal address = view only (G3).
- Mac (A7, macOS 26 guide): «Файл» > «Новая подписка на календарь» > paste the address > «Подписаться» > «Имя», «Расположение»
  (iCloud = all devices, «На моем Mac» = this Mac only), «Автообновление» > «OK». "Вы не можете редактировать календари, на
  которые Вы подписаны" (A7, A9). The macOS 27 guide has no subscription page (icl1022 without version redirects to its start page).
- iPhone «Добавить подписной календарь» path: not verified.

## 6. Option C: public calendar (not recommended)
- Checkbox «Сделать общедоступным» in «Разрешения на доступ к мероприятиям» (G4).
- Then anyone can find it on a website, search engines may index it, anyone can sync or subscribe to it (G4).
- Workspace admins may restrict sharing settings (G4). Product verdict "not recommended (privacy)" is ours.

## 7. Troubleshooting (official items only)
- Event missing on Mac: calendar checkbox off; account not set up/turned off in «Учетные записи интернета»; check other dates,
  «Вид» > «Показать события на весь день», time zone, Mac «Дата и время» (A8).
- Google calendar not shown at all: enable it at syncselect (G1).
- Sign-in fails on an older OS with 2-step verification: use an app password (G2).
- School/work account: secret address or public sharing may be disabled by the admin (G3, G4).
- Duplicates when both A and B are used: no official page found (our note: use only one option).

## 8. Editing on Apple devices
- Account sync (option A) is "view and edit, changes sync" (G3). Subscribed calendars (option B) are read-only (A7, A9).
- **Product rule (ours, not from these pages):** local app data is the source of truth; edits made in Apple Calendar are not
  read back and may be overwritten by the next sync.

## Links for the help screen
All query-less, fetched 2026-10-16. Google pages without `?hl=` showed English to the fetcher (Google picks the language).
| Key | URL | Caption |
|---|---|---|
| apple-iphone-accounts | https://support.apple.com/ru-ru/guide/iphone/ipha0d932e96/ios | Как добавить учетную запись календаря на iPhone |
| apple-mac-internet-accounts | https://support.apple.com/ru-ru/guide/mac-help/mh35565/mac | Учетные записи интернета на Mac |
| apple-mac-calendar-accounts | https://support.apple.com/ru-ru/guide/calendar/icl4308d6701/mac | Добавление учетной записи в Календаре на Mac |
| apple-mac-subscribe | https://support.apple.com/ru-ru/guide/calendar/icl1022/16.0/mac/26 | Подписка на календарь на Mac |
| google-apple-sync | https://support.google.com/calendar/answer/99358 | Google Календарь в Календаре Apple |
| google-ical-address | https://support.google.com/calendar/answer/37648 | Закрытый адрес календаря в формате iCal |

## Not verified
- Apple iPhone/iPad guide pages (A2 and all iPhone/iPad guide pages): the fetched text is cut off in the navigation before the
  article, so the current iOS 26/27 Calendar path, the «Учетные записи Календаря» label, «Календари» toggle wording,
  «Добавить подписной календарь» and the «Календари» button in the app are not confirmed.
- iOS fetch/push ("Fetch New Data") settings and their labels.
- Refresh interval of a subscribed Google iCal address (no Google figure; Apple lists no intervals).
- Mac Internet Accounts toggle label «Календари».
- Whether the app-created calendar is under «Мои календари» (auto-synced) or needs syncselect.
- Duplicates when options A and B are combined.
