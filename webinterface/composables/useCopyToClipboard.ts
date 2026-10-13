export const COPY_FAILED_MESSAGE = 'Could not copy automatically — select the text and copy it manually';

/**
 * Returns a function that copies text and reports the outcome as a toast.
 *
 * Over plain http (docker-compose's default) navigator.clipboard does not
 * exist at all — the API is absent, not failing — so it is checked before use
 * and the user is told to copy by hand instead of getting a TypeError.
 * Call it in setup: the toast needs the component's injection context.
 */
export const useCopyToClipboard = () => {
  const toast = useAppToast();

  return async (text: string, successMessage = 'Copied to clipboard'): Promise<boolean> => {
    if (!navigator.clipboard) {
      toast.error(COPY_FAILED_MESSAGE);
      return false;
    }
    try {
      await navigator.clipboard.writeText(text);
      toast.success(successMessage);
      return true;
    } catch {
      toast.error(COPY_FAILED_MESSAGE);
      return false;
    }
  };
};
