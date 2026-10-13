/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Trigger a browser "save as" for arbitrary in-memory content. Centralizes the
 * object-URL lifecycle (create → click → revoke) so callers cannot leak URLs.
 *
 * @param data - Blob or raw parts to wrap in a Blob.
 * @param filename - Suggested download filename.
 * @param mimeType - MIME type used only when `data` is not already a Blob.
 */
export function downloadBlob(
  data: BlobPart | Blob,
  filename: string,
  mimeType = 'application/octet-stream'
): void {
  const blob =
    data instanceof Blob ? data : new Blob([data], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}
