<template>
  <div>
    <h2 class="text-2xl font-semibold text-surface-900 dark:text-surface-0 mb-1">Import & Export</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">Import calendars and contacts from files, or export your data for backup.</p>

    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="import">
          <div class="flex items-center gap-2">
            <i class="pi pi-upload" />
            <span>Import</span>
          </div>
        </Tab>
        <Tab value="export">
          <div class="flex items-center gap-2">
            <i class="pi pi-download" />
            <span>Export</span>
          </div>
        </Tab>
      </TabList>
      <TabPanels>
        <!-- Import Tab -->
        <TabPanel value="import">
          <div class="space-y-8 pt-4">
            <!-- Import Calendars -->
            <div>
              <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-1">
                <i class="pi pi-calendar mr-2 text-primary-500" />Import Calendars
              </h3>
              <p class="text-sm text-surface-500 dark:text-surface-400 mb-4">Upload an .ics file to import events into a calendar.</p>

              <!-- File drop zone -->
              <div
                @dragover.prevent="calDragOver = true"
                @dragleave="calDragOver = false"
                @drop.prevent="handleCalendarDrop"
                @click="calFileInput?.click()"
                :class="[
                  'border-2 border-dashed rounded-xl p-8 text-center cursor-pointer transition-colors',
                  calDragOver
                    ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/10'
                    : calFile
                      ? 'border-green-400 dark:border-green-600 bg-green-50 dark:bg-green-900/10'
                      : 'border-surface-300 dark:border-surface-600 hover:border-surface-400 dark:hover:border-surface-500'
                ]"
              >
                <input ref="calFileInput" type="file" accept=".ics" class="hidden" @change="handleCalendarFileChange" />
                <div v-if="calFile" class="flex items-center justify-center gap-3">
                  <i class="pi pi-file text-2xl text-green-500" />
                  <div class="text-left">
                    <p class="font-medium text-surface-900 dark:text-surface-0">{{ calFile.name }}</p>
                    <p class="text-sm text-surface-500">{{ formatFileSize(calFile.size) }}</p>
                  </div>
                  <Button icon="pi pi-times" severity="secondary" text rounded size="small" @click.stop="calFile = null" />
                </div>
                <div v-else>
                  <i class="pi pi-calendar-plus text-3xl text-surface-400 dark:text-surface-500 mb-2 block" />
                  <p class="text-surface-600 dark:text-surface-400">Drag and drop an .ics file here or click to browse</p>
                </div>
              </div>

              <div v-if="calFile" class="mt-4 space-y-4">
                <div>
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Import to Calendar</label>
                  <Select
                    v-model="selectedCalendar"
                    :options="calendarStore.calendars"
                    option-label="name"
                    placeholder="Select a calendar"
                    class="w-full"
                  />
                </div>

                <div>
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Duplicate Handling</label>
                  <SelectButton
                    v-model="calDuplicateAction"
                    :options="duplicateOptions"
                    option-label="label"
                    option-value="value"
                  />
                </div>

                <Button
                  label="Import Calendar"
                  icon="pi pi-upload"
                  :loading="calImporting"
                  :disabled="!selectedCalendar"
                  @click="executeCalendarImport"
                />
              </div>
            </div>

            <Divider />

            <!-- Import Contacts -->
            <div>
              <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-1">
                <i class="pi pi-users mr-2 text-primary-500" />Import Contacts
              </h3>
              <p class="text-sm text-surface-500 dark:text-surface-400 mb-4">Upload a .vcf file to import contacts into an address book.</p>

              <div
                @dragover.prevent="vcfDragOver = true"
                @dragleave="vcfDragOver = false"
                @drop.prevent="handleContactDrop"
                @click="vcfFileInput?.click()"
                :class="[
                  'border-2 border-dashed rounded-xl p-8 text-center cursor-pointer transition-colors',
                  vcfDragOver
                    ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/10'
                    : vcfFile
                      ? 'border-green-400 dark:border-green-600 bg-green-50 dark:bg-green-900/10'
                      : 'border-surface-300 dark:border-surface-600 hover:border-surface-400 dark:hover:border-surface-500'
                ]"
              >
                <input ref="vcfFileInput" type="file" accept=".vcf" class="hidden" @change="handleContactFileChange" />
                <div v-if="vcfFile" class="flex items-center justify-center gap-3">
                  <i class="pi pi-file text-2xl text-green-500" />
                  <div class="text-left">
                    <p class="font-medium text-surface-900 dark:text-surface-0">{{ vcfFile.name }}</p>
                    <p class="text-sm text-surface-500">{{ formatFileSize(vcfFile.size) }}</p>
                  </div>
                  <Button icon="pi pi-times" severity="secondary" text rounded size="small" @click.stop="vcfFile = null" />
                </div>
                <div v-else>
                  <i class="pi pi-id-card text-3xl text-surface-400 dark:text-surface-500 mb-2 block" />
                  <p class="text-surface-600 dark:text-surface-400">Drag and drop a .vcf file here or click to browse</p>
                </div>
              </div>

              <div v-if="vcfFile" class="mt-4 space-y-4">
                <div>
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Import to Address Book</label>
                  <Select
                    v-model="selectedAddressBook"
                    :options="contactsStore.addressBooks"
                    option-label="Name"
                    placeholder="Select an address book"
                    class="w-full"
                  />
                </div>

                <div>
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Duplicate Handling</label>
                  <SelectButton
                    v-model="vcfDuplicateAction"
                    :options="duplicateOptions"
                    option-label="label"
                    option-value="value"
                  />
                </div>

                <Button
                  label="Import Contacts"
                  icon="pi pi-upload"
                  :loading="vcfImporting"
                  :disabled="!selectedAddressBook"
                  @click="executeContactImport"
                />
              </div>
            </div>
          </div>
        </TabPanel>

        <!-- Export Tab -->
        <TabPanel value="export">
          <div class="space-y-8 pt-4">
            <!-- Export Calendars -->
            <div>
              <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-1">
                <i class="pi pi-calendar mr-2 text-primary-500" />Export Calendars
              </h3>
              <p class="text-sm text-surface-500 dark:text-surface-400 mb-4">Download a calendar as an .ics file.</p>

              <div class="flex flex-wrap items-end gap-3">
                <div class="flex-1 min-w-[200px]">
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Calendar</label>
                  <Select
                    v-model="exportCalendar"
                    :options="calendarStore.calendars"
                    option-label="name"
                    placeholder="Select a calendar"
                    class="w-full"
                  />
                </div>
                <Button
                  label="Export .ics"
                  icon="pi pi-download"
                  :loading="calExporting"
                  :disabled="!exportCalendar"
                  @click="exportCalendarIcs"
                />
              </div>
            </div>

            <Divider />

            <!-- Export Contacts -->
            <div>
              <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-1">
                <i class="pi pi-users mr-2 text-primary-500" />Export Contacts
              </h3>
              <p class="text-sm text-surface-500 dark:text-surface-400 mb-4">Download an address book as a .vcf file.</p>

              <div class="flex flex-wrap items-end gap-3">
                <div class="flex-1 min-w-[200px]">
                  <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Address Book</label>
                  <Select
                    v-model="exportAddressBook"
                    :options="contactsStore.addressBooks"
                    option-label="Name"
                    placeholder="Select an address book"
                    class="w-full"
                  />
                </div>
                <Button
                  label="Export .vcf"
                  icon="pi pi-download"
                  :loading="vcfExporting"
                  :disabled="!exportAddressBook"
                  @click="exportContactsVcf"
                />
              </div>
            </div>

            <Divider />

            <!-- Full Backup -->
            <div>
              <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-1">
                <i class="pi pi-box mr-2 text-primary-500" />Full Backup
              </h3>
              <p class="text-sm text-surface-500 dark:text-surface-400 mb-4">
                Download all your calendars and contacts as a single .zip archive.
              </p>
              <Button
                label="Download Backup"
                icon="pi pi-download"
                :loading="backupExporting"
                @click="exportBackup"
              />
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <!-- Import Results Dialog -->
    <Dialog
      v-model:visible="showResults"
      header="Import Results"
      :modal="true"
      class="w-full max-w-lg"
    >
      <div v-if="importResult" class="space-y-3">
        <div class="flex items-center gap-3 p-3 bg-green-50 dark:bg-green-900/20 rounded-lg text-green-700 dark:text-green-400">
          <i class="pi pi-check-circle text-lg" />
          <span>{{ importResult.imported }} imported successfully</span>
        </div>
        <div v-if="importResult.skipped > 0" class="flex items-center gap-3 p-3 bg-yellow-50 dark:bg-yellow-900/20 rounded-lg text-yellow-700 dark:text-yellow-400">
          <i class="pi pi-minus-circle text-lg" />
          <span>{{ importResult.skipped }} skipped (duplicates)</span>
        </div>
        <div v-if="importResult.failed > 0" class="flex items-center gap-3 p-3 bg-red-50 dark:bg-red-900/20 rounded-lg text-red-700 dark:text-red-400">
          <i class="pi pi-times-circle text-lg" />
          <span>{{ importResult.failed }} failed</span>
        </div>

        <div v-if="importResult.errors && importResult.errors.length > 0" class="mt-4">
          <h4 class="text-sm font-medium text-surface-700 dark:text-surface-300 mb-2">Error Details</h4>
          <div class="bg-surface-50 dark:bg-surface-800 rounded-lg p-3 max-h-48 overflow-y-auto">
            <ul class="list-disc list-inside text-sm text-surface-600 dark:text-surface-400 space-y-1">
              <li v-for="(err, index) in importResult.errors" :key="index">
                <span v-if="err.summary" class="font-medium">{{ err.summary }}: </span>{{ err.error }}
              </li>
            </ul>
          </div>
        </div>
      </div>

      <template #footer>
        <Button label="Close" @click="showResults = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';
import type { ImportResult } from '~/types/importexport';

definePageMeta({ layout: 'settings', middleware: 'auth' });

const calendarStore = useCalendarStore();
const contactsStore = useContactsStore();
const toast = useAppToast();
const api = useApi();

const activeTab = ref('import');

// Import: Calendar
const calFileInput = ref<HTMLInputElement | null>(null);
const calFile = ref<File | null>(null);
const calDragOver = ref(false);
const selectedCalendar = ref<Calendar | null>(null);
const calDuplicateAction = ref('skip');
const calImporting = ref(false);

// Import: Contacts
const vcfFileInput = ref<HTMLInputElement | null>(null);
const vcfFile = ref<File | null>(null);
const vcfDragOver = ref(false);
const selectedAddressBook = ref<AddressBook | null>(null);
const vcfDuplicateAction = ref('skip');
const vcfImporting = ref(false);

// Export
const exportCalendar = ref<Calendar | null>(null);
const calExporting = ref(false);
const exportAddressBook = ref<AddressBook | null>(null);
const vcfExporting = ref(false);
const backupExporting = ref(false);

// Results dialog
const showResults = ref(false);
const importResult = ref<ImportResult | null>(null);

const duplicateOptions = [
  { label: 'Skip', value: 'skip' },
  { label: 'Replace', value: 'replace' },
  { label: 'Duplicate', value: 'duplicate' },
];

// Load calendars and address books
onMounted(async () => {
  await Promise.all([
    calendarStore.fetchCalendars(),
    contactsStore.fetchAddressBooks(),
  ]);
});

// File handling
function handleCalendarDrop(e: DragEvent) {
  calDragOver.value = false;
  const file = e.dataTransfer?.files[0];
  if (file && file.name.endsWith('.ics')) {
    calFile.value = file;
  } else {
    toast.error('Please drop an .ics file');
  }
}

function handleCalendarFileChange(e: Event) {
  const input = e.target as HTMLInputElement;
  calFile.value = input.files?.[0] ?? null;
  input.value = '';
}

function handleContactDrop(e: DragEvent) {
  vcfDragOver.value = false;
  const file = e.dataTransfer?.files[0];
  if (file && file.name.endsWith('.vcf')) {
    vcfFile.value = file;
  } else {
    toast.error('Please drop a .vcf file');
  }
}

function handleContactFileChange(e: Event) {
  const input = e.target as HTMLInputElement;
  vcfFile.value = input.files?.[0] ?? null;
  input.value = '';
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Import actions
async function executeCalendarImport() {
  if (!calFile.value || !selectedCalendar.value) return;
  calImporting.value = true;
  try {
    const formData = new FormData();
    formData.append('file', calFile.value);

    const result = await api<ImportResult>(
      `/api/v1/calendars/${selectedCalendar.value.uuid}/import?duplicate_handling=${calDuplicateAction.value}`,
      { method: 'POST', body: formData },
    );

    importResult.value = result;
    showResults.value = true;
    calFile.value = null;
    await calendarStore.fetchCalendars();
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : 'Failed to import calendar';
    toast.error(message);
  } finally {
    calImporting.value = false;
  }
}

async function executeContactImport() {
  if (!vcfFile.value || !selectedAddressBook.value) return;
  vcfImporting.value = true;
  try {
    const formData = new FormData();
    formData.append('file', vcfFile.value);

    const result = await api<ImportResult>(
      `/api/v1/addressbooks/${selectedAddressBook.value.ID}/import?duplicate_handling=${vcfDuplicateAction.value}`,
      { method: 'POST', body: formData },
    );

    importResult.value = result;
    showResults.value = true;
    vcfFile.value = null;
    await contactsStore.fetchAddressBooks();
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : 'Failed to import contacts';
    toast.error(message);
  } finally {
    vcfImporting.value = false;
  }
}

// Export actions
async function exportCalendarIcs() {
  if (!exportCalendar.value) return;
  calExporting.value = true;
  try {
    const blob = await api<Blob>(
      `/api/v1/calendars/${exportCalendar.value.uuid}/export`,
      { responseType: 'blob' },
    );
    downloadBlob(blob, `${exportCalendar.value.name}.ics`);
    toast.success('Calendar exported');
  } catch {
    toast.error('Failed to export calendar');
  } finally {
    calExporting.value = false;
  }
}

async function exportContactsVcf() {
  if (!exportAddressBook.value) return;
  vcfExporting.value = true;
  try {
    const blob = await api<Blob>(
      `/api/v1/addressbooks/${exportAddressBook.value.ID}/export`,
      { responseType: 'blob' },
    );
    downloadBlob(blob, `${exportAddressBook.value.Name}.vcf`);
    toast.success('Contacts exported');
  } catch {
    toast.error('Failed to export contacts');
  } finally {
    vcfExporting.value = false;
  }
}

async function exportBackup() {
  backupExporting.value = true;
  try {
    const blob = await api<Blob>(
      '/api/v1/users/me/export',
      { responseType: 'blob' },
    );
    downloadBlob(blob, 'calcard-backup.zip');
    toast.success('Backup downloaded');
  } catch {
    toast.error('Failed to export backup');
  } finally {
    backupExporting.value = false;
  }
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
</script>
