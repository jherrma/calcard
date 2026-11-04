# Release notes: tasks and task lists (#170)

Draft for the GitHub release that ships #170. Paste it into the release body.

## New

- **Tasks in the web UI.** A new "Tasks" section, between Calendar and Contacts, with an overdue badge. You can
  create task lists, add tasks with quick-add or a form, edit them, tick them off (with undo), filter, search and
  sort them. The dashboard shows an "Open tasks" figure, and search sends task-list hits to the Tasks section.
- **Task lists are their own collections.** A task list is a CalDAV collection that advertises only `VTODO`, so
  task apps offer it as a task list and calendar apps leave it out. A calendar advertises only `VEVENT`.
- **New accounts** get two collections: "Personal" (events) and "Tasks" (a task list). A calendar created over
  the REST API without `kind` is an event calendar.
- **REST API:** `GET/POST /api/v1/calendars/:calendar_id/tasks`, `GET/PATCH/DELETE …/tasks/:task_id`,
  `POST …/complete` and `…/uncomplete`, `GET /api/v1/tasks` (across all lists) and
  `GET /api/v1/tasks/summary?as_of=…`. `POST /api/v1/calendars` takes `kind` (`events`, `tasks` or `both`), and
  every calendar response has a read-only `kind`.
- **MCP (AI assistants):** seven task tools — `list_tasks`, `create_task`, `update_task`, `complete_task`,
  `uncomplete_task`, `delete_task` and `get_task_summary` — use the same rules as the REST API (a repeating task
  cannot be completed, deleting a task is permanent). `create_calendar` takes `kind`, and `update_calendar` can widen
  a calendar or task list to `both`. The event tools never return tasks.

## Upgrading: what happens to your existing calendars

Before this release, every calendar accepted both events and tasks (`VEVENT,VTODO`).

1. **The first start narrows each existing calendar to what it holds.** This runs once:
   - A calendar that holds only events becomes an **event calendar** (`VEVENT`).
   - A calendar that holds only tasks becomes a **task list** (`VTODO`). This includes a "Personal" calendar
     that holds only reminders synced from a phone: it moves from the Calendar section to Tasks.
   - Calendars that are empty or hold both events and tasks stay mixed, so they keep both. Subscribed calendars
     and calendars created after the upgrade are never changed.

   When it changes anything, the server logs
   `data migration: narrowed the component set of legacy calendars to what they hold (#170) event_calendars=N task_lists=M`.
   A `system_settings` marker (`migration.narrow_component_sets.v1`) keeps it from running again, so a calendar
   you widen later stays widened.
2. **After the migration, a calendar that held only events refuses tasks.** A CalDAV client that tries to save a
   task (`VTODO`) in it gets **403** with the CalDAV precondition `supported-calendar-component`. A narrowed task
   list refuses events the same way. Thunderbird reports this as
   `CalDAV: Unexpected status adding item to <calendar>: 403` and does not keep the item.
3. **Clients see the change only after they re-read the collection, not on a normal sync.** The migration bumps
   each changed calendar's ctag and sync token, but that alone does not make a client re-read which components the
   calendar accepts. Until it does, the client still offers the old choice and the server refuses the write
   (point 2).
   - **DAVx⁵:** open the account and use **"Refresh collection list"**.
   - **Thunderbird:** **restart Thunderbird**. Verified with Thunderbird 156: "Synchronize", and disabling and
     re-enabling the calendar, are not enough.
   - **Apple Calendar / Reminders:** not verified yet (see the table below).
4. **Turning a narrowed calendar back into a mixed one is API-only.** The web UI has no option for it. Send
   `PATCH /api/v1/calendars/<calendar uuid>` with `{"kind":"both"}`. The UUID is in `GET /api/v1/calendars`
   and in the calendar's CalDAV URL. Narrowing a calendar (any change that drops events or tasks) is refused with
   **409**.

   ```bash
   TOKEN=$(curl -s -X POST https://calcard.example.com/api/v1/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"email":"you@example.com","password":"…"}' | jq -r .data.access_token)
   curl -X PATCH "https://calcard.example.com/api/v1/calendars/<calendar uuid>" \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"kind":"both"}'
   ```

   Then refresh the collection in your clients (point 3). You can also re-create the calendar from a DAV client.

## Other behaviour changes

- **Deleting a task is permanent.** This applies whether you delete it in the web UI, over the REST API or from a
  DAV client. Nothing can be restored.
- **Repeating tasks cannot be completed in the web UI or over REST yet.** You get **409** `Repeating tasks cannot be
  completed here yet. Complete it in your task app.` You can still edit their title, notes, priority and due
  date, and the repeat rule is kept.
- **VTODOs imported into an event-only calendar are skipped.** This includes the default "Personal" calendar of a
  new account. The import counts them under `skipped` and names them in `errors`; import them into a task list
  instead.
- **The "last calendar" guard is per kind.** `cannot delete your last calendar` now protects the last calendar that
  can hold events. With "Personal" and "Tasks" you can no longer delete "Personal" (the previous version allowed
  it); a task list can always be deleted.
- **Event text over REST and MCP is plain text.** A summary, description or location is returned and accepted as
  written (`Buy milk, eggs`); the previous version returned the iCalendar escapes (`Buy milk\, eggs`, `\;`, `\n`).
  A client that works around the escapes should stop doing so.
- Event endpoints and the MCP event tools treat a task's UUID as unknown (the REST event routes answer 404).
  Event searches and event counts include events only.

## Client compatibility (interop pass, 2026-11-03)

| Client | Result |
| --- | --- |
| Thunderbird 156.0.1 (Linux) | **Pass.** Task lists appear in the Tasks tab with their colours. Event calendars cannot hold tasks: the New Task dialog does not offer them, and quick-add says "Please select a calendar that supports tasks". Thunderbird's left-hand calendar list is shared by its Calendar and Tasks tabs, so it always lists every calendar. Create, complete, un-complete, priority, due date and delete work in both directions. A repeating task stays repeating after a web edit. A subtask (`RELATED-TO`), a reminder (`VALARM`) and a custom `X-` property written by Thunderbird survive a web edit. Thunderbird filters "Next Seven Days" from its own copy of the tasks; it never sends a date-range query to the server. Mixed legacy calendars keep working. A narrowed calendar is picked up after a restart. |
| evolution-data-server 3.60.2 (WebDAV discovery, as used by Evolution / GNOME Online Accounts) | **Pass** for discovery only. Task lists are offered as task lists only, event calendars as calendars only, and mixed calendars as both. The Evolution app itself was not tested. |
| Apple Reminders / Calendar (iOS, macOS) | **Not verified.** No Apple device was available. |
| Tasks.org via DAVx⁵ (Android) | **Not verified.** No Android device or emulator was available. |
