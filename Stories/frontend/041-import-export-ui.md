# Story 041: Import/Export UI

## Story
**As a** user
**I want to** import and export my calendars and contacts through the web interface
**So that** I can migrate data from other services or create backups

## Acceptance Criteria

### Import Functionality
- [ ] File upload component accepts .ics files for calendar import
- [ ] File upload component accepts .vcf files for contact import
- [ ] Drag-and-drop file upload is supported
- [ ] Progress indicator shown during import processing
- [ ] Import preview shows number of items to be imported
- [ ] User can select target calendar/addressbook for import
- [ ] Option to create new calendar/addressbook during import
- [ ] Import summary shows successful/failed/skipped items
- [ ] Detailed error messages for failed imports
- [ ] Duplicate detection with merge/skip/replace options

### Export Functionality
- [ ] Export single calendar as .ics file
- [ ] Export all calendars as single .ics file
- [ ] Export single addressbook as .vcf file
- [ ] Export all contacts as single .vcf file
- [ ] Date range filter for calendar export
- [ ] Export includes all event/contact details
- [ ] Download triggers automatically after generation
- [ ] Export progress indicator for large datasets

### UI Components
- [ ] Import/Export page accessible from settings
- [ ] Tabbed interface for Import vs Export
- [ ] Clear file type indicators and instructions
- [ ] Cancel button for in-progress operations
- [ ] Success/error toast notifications

## Technical Details

### Import Page Component
```vue
<template>
  <div class="import-export-page">
    <h1>Import & Export</h1>

    <TabView v-model:activeIndex="activeTab">
      <TabPanel header="Import">
        <div class="import-section">
          <h2>Import Calendars</h2>
          <FileUpload
            mode="advanced"
            accept=".ics"
            :maxFileSize="10000000"
            :customUpload="true"
            @uploader="handleCalendarImport"
            @select="previewCalendarImport"
          >
            <template #empty>
              <div class="upload-placeholder">
                <i class="pi pi-calendar-plus"></i>
                <p>Drag and drop .ics files here or click to browse</p>
              </div>
            </template>
          </FileUpload>

          <div v-if="calendarPreview" class="import-preview">
            <h3>Import Preview</h3>
            <p>{{ calendarPreview.eventCount }} events found</p>

            <div class="field">
              <label>Import to Calendar</label>
              <Dropdown
                v-model="selectedCalendar"
                :options="calendarOptions"
                optionLabel="name"
                optionValue="id"
                placeholder="Select a calendar"
              />
            </div>

            <div class="field-checkbox">
              <Checkbox v-model="createNewCalendar" binary inputId="newCal" />
              <label for="newCal">Create new calendar</label>
            </div>

            <InputText
              v-if="createNewCalendar"
              v-model="newCalendarName"
              placeholder="New calendar name"
            />

            <div class="field">
              <label>Duplicate Handling</label>
              <SelectButton
                v-model="duplicateAction"
                :options="duplicateOptions"
                optionLabel="label"
                optionValue="value"
              />
            </div>

            <Button
              label="Import"
              icon="pi pi-upload"
              :loading="importing"
              @click="executeCalendarImport"
            />
          </div>

          <Divider />

          <h2>Import Contacts</h2>
          <FileUpload
            mode="advanced"
            accept=".vcf"
            :maxFileSize="10000000"
            :customUpload="true"
            @uploader="handleContactImport"
            @select="previewContactImport"
          >
            <template #empty>
              <div class="upload-placeholder">
                <i class="pi pi-users"></i>
                <p>Drag and drop .vcf files here or click to browse</p>
              </div>
            </template>
          </FileUpload>

          <div v-if="contactPreview" class="import-preview">
            <h3>Import Preview</h3>
            <p>{{ contactPreview.contactCount }} contacts found</p>

            <div class="field">
              <label>Import to Address Book</label>
              <Dropdown
                v-model="selectedAddressbook"
                :options="addressbookOptions"
                optionLabel="name"
                optionValue="id"
                placeholder="Select an address book"
              />
            </div>

            <Button
              label="Import"
              icon="pi pi-upload"
              :loading="importing"
              @click="executeContactImport"
            />
          </div>
        </div>
      </TabPanel>

      <TabPanel header="Export">
        <div class="export-section">
          <h2>Export Calendars</h2>

          <div class="export-options">
            <div class="field">
              <label>Select Calendars</label>
              <MultiSelect
                v-model="exportCalendars"
                :options="calendars"
                optionLabel="name"
                optionValue="id"
                placeholder="Select calendars to export"
                display="chip"
              />
            </div>

            <div class="field">
              <label>Date Range (optional)</label>
              <div class="date-range">
                <Calendar v-model="exportStartDate" placeholder="Start date" />
                <span>to</span>
                <Calendar v-model="exportEndDate" placeholder="End date" />
              </div>
            </div>

            <Button
              label="Export Calendars"
              icon="pi pi-download"
              :loading="exporting"
              :disabled="exportCalendars.length === 0"
              @click="exportCalendarsIcs"
            />
          </div>

          <Divider />

          <h2>Export Contacts</h2>

          <div class="export-options">
            <div class="field">
              <label>Select Address Books</label>
              <MultiSelect
                v-model="exportAddressbooks"
                :options="addressbooks"
                optionLabel="name"
                optionValue="id"
                placeholder="Select address books to export"
                display="chip"
              />
            </div>

            <Button
              label="Export Contacts"
              icon="pi pi-download"
              :loading="exporting"
              :disabled="exportAddressbooks.length === 0"
              @click="exportContactsVcf"
            />
          </div>
        </div>
      </TabPanel>
    </TabView>

    <!-- Import Results Dialog -->
    <Dialog
      v-model:visible="showResults"
      header="Import Results"
      :modal="true"
      :closable="true"
    >
      <div class="import-results">
        <div class="result-stat success">
          <i class="pi pi-check-circle"></i>
          <span>{{ importResults.successful }} imported successfully</span>
        </div>
        <div v-if="importResults.skipped > 0" class="result-stat warning">
          <i class="pi pi-minus-circle"></i>
          <span>{{ importResults.skipped }} skipped (duplicates)</span>
        </div>
        <div v-if="importResults.failed > 0" class="result-stat error">
          <i class="pi pi-times-circle"></i>
          <span>{{ importResults.failed }} failed</span>
        </div>

        <div v-if="importResults.errors.length > 0" class="error-details">
          <h4>Error Details</h4>
          <ul>
            <li v-for="(error, index) in importResults.errors" :key="index">
              {{ error }}
            </li>
          </ul>
        </div>
      </div>

      <template #footer>
        <Button label="Close" @click="showResults = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useCalendarStore } from '~/stores/calendars'
import { useContactStore } from '~/stores/contacts'
import { useToast } from 'primevue/usetoast'

const calendarStore = useCalendarStore()
const contactStore = useContactStore()
const toast = useToast()

const activeTab = ref(0)

// Import state
const calendarPreview = ref<{ eventCount: number; file: File } | null>(null)
const contactPreview = ref<{ contactCount: number; file: File } | null>(null)
const selectedCalendar = ref<string | null>(null)
const selectedAddressbook = ref<string | null>(null)
const createNewCalendar = ref(false)
const newCalendarName = ref('')
const duplicateAction = ref('skip')
const importing = ref(false)

const duplicateOptions = [
  { label: 'Skip', value: 'skip' },
  { label: 'Replace', value: 'replace' },
  { label: 'Merge', value: 'merge' }
]

// Export state
const exportCalendars = ref<string[]>([])
const exportAddressbooks = ref<string[]>([])
const exportStartDate = ref<Date | null>(null)
const exportEndDate = ref<Date | null>(null)
const exporting = ref(false)

// Results
const showResults = ref(false)
const importResults = ref({
  successful: 0,
  skipped: 0,
  failed: 0,
  errors: [] as string[]
})

const calendars = computed(() => calendarStore.calendars)
const addressbooks = computed(() => contactStore.addressbooks)

const calendarOptions = computed(() => [
  ...calendars.value,
  ...(createNewCalendar.value ? [] : [])
])

const addressbookOptions = computed(() => addressbooks.value)

onMounted(async () => {
  await Promise.all([
    calendarStore.fetchCalendars(),
    contactStore.fetchAddressbooks()
  ])
})

async function previewCalendarImport(event: any) {
  const file = event.files[0]
  try {
    const { data } = await useApi().post('/api/v1/import/calendar/preview', file, {
      headers: { 'Content-Type': 'text/calendar' }
    })
    calendarPreview.value = { eventCount: data.eventCount, file }
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Preview Failed',
      detail: 'Could not parse the calendar file',
      life: 5000
    })
  }
}

async function executeCalendarImport() {
  if (!calendarPreview.value) return

  importing.value = true
  try {
    const formData = new FormData()
    formData.append('file', calendarPreview.value.file)
    formData.append('calendarId', createNewCalendar.value ? '' : selectedCalendar.value || '')
    formData.append('newCalendarName', createNewCalendar.value ? newCalendarName.value : '')
    formData.append('duplicateAction', duplicateAction.value)

    const { data } = await useApi().post('/api/v1/import/calendar', formData)

    importResults.value = data
    showResults.value = true
    calendarPreview.value = null

    await calendarStore.fetchCalendars()
  } catch (error: any) {
    toast.add({
      severity: 'error',
      summary: 'Import Failed',
      detail: error.response?.data?.error || 'Failed to import calendar',
      life: 5000
    })
  } finally {
    importing.value = false
  }
}

async function previewContactImport(event: any) {
  const file = event.files[0]
  try {
    const { data } = await useApi().post('/api/v1/import/contacts/preview', file, {
      headers: { 'Content-Type': 'text/vcard' }
    })
    contactPreview.value = { contactCount: data.contactCount, file }
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Preview Failed',
      detail: 'Could not parse the contacts file',
      life: 5000
    })
  }
}

async function executeContactImport() {
  if (!contactPreview.value) return

  importing.value = true
  try {
    const formData = new FormData()
    formData.append('file', contactPreview.value.file)
    formData.append('addressbookId', selectedAddressbook.value || '')

    const { data } = await useApi().post('/api/v1/import/contacts', formData)

    importResults.value = data
    showResults.value = true
    contactPreview.value = null

    await contactStore.fetchAddressbooks()
  } catch (error: any) {
    toast.add({
      severity: 'error',
      summary: 'Import Failed',
      detail: error.response?.data?.error || 'Failed to import contacts',
      life: 5000
    })
  } finally {
    importing.value = false
  }
}

async function exportCalendarsIcs() {
  exporting.value = true
  try {
    const params = new URLSearchParams()
    exportCalendars.value.forEach(id => params.append('calendarIds', id))
    if (exportStartDate.value) params.append('startDate', exportStartDate.value.toISOString())
    if (exportEndDate.value) params.append('endDate', exportEndDate.value.toISOString())

    const response = await useApi().get(`/api/v1/export/calendars?${params}`, {
      responseType: 'blob'
    })

    downloadFile(response.data, 'calendars.ics', 'text/calendar')

    toast.add({
      severity: 'success',
      summary: 'Export Complete',
      detail: 'Calendar export downloaded',
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Export Failed',
      detail: 'Failed to export calendars',
      life: 5000
    })
  } finally {
    exporting.value = false
  }
}

async function exportContactsVcf() {
  exporting.value = true
  try {
    const params = new URLSearchParams()
    exportAddressbooks.value.forEach(id => params.append('addressbookIds', id))

    const response = await useApi().get(`/api/v1/export/contacts?${params}`, {
      responseType: 'blob'
    })

    downloadFile(response.data, 'contacts.vcf', 'text/vcard')

    toast.add({
      severity: 'success',
      summary: 'Export Complete',
      detail: 'Contacts export downloaded',
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Export Failed',
      detail: 'Failed to export contacts',
      life: 5000
    })
  } finally {
    exporting.value = false
  }
}

function downloadFile(blob: Blob, filename: string, mimeType: string) {
  const url = window.URL.createObjectURL(new Blob([blob], { type: mimeType }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}
</script>

<style scoped>
.import-export-page {
  max-width: 800px;
  margin: 0 auto;
  padding: 2rem;
}

.upload-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 2rem;
  color: var(--text-color-secondary);
}

.upload-placeholder i {
  font-size: 3rem;
  margin-bottom: 1rem;
}

.import-preview {
  margin-top: 1.5rem;
  padding: 1.5rem;
  background: var(--surface-ground);
  border-radius: 8px;
}

.export-options {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.date-range {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.import-results {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.result-stat {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem;
  border-radius: 4px;
}

.result-stat.success {
  background: var(--green-100);
  color: var(--green-700);
}

.result-stat.warning {
  background: var(--yellow-100);
  color: var(--yellow-700);
}

.result-stat.error {
  background: var(--red-100);
  color: var(--red-700);
}

.error-details {
  margin-top: 1rem;
  padding: 1rem;
  background: var(--surface-ground);
  border-radius: 4px;
}

.error-details ul {
  margin: 0;
  padding-left: 1.5rem;
}
</style>
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 038 (Settings Pages) - navigation integration
- Backend Story 029 (Import/Export Functionality)

## Estimation
- **Complexity:** Medium
- **Components:** 1 main page, 2 tab panels, 1 dialog

## Notes
- File size limits should match backend configuration
- Large imports should show progress indication
- Consider chunked upload for very large files
- Export should handle timeout for large datasets
