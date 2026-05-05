import { useState, useEffect, useCallback, useMemo } from 'react'

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { getFilesPaginated, getFileViewUrl, getThumbnailUrl, getSDCardFiles, getSDCardPreviewUrl } from '../api.js'
import { Card } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'

const PAGE_SIZE = 40
const IMAGE_EXTENSIONS = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'tiff', 'tif', 'heic', 'heif', 'avif'])

function isImageFile(name) {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  return IMAGE_EXTENSIONS.has(ext)
}

function formatFileSize(bytes) {
  if (bytes === 0 || bytes == null) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let idx = 0
  let size = Math.abs(bytes)
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${idx === 0 ? size : size.toFixed(1)} ${units[idx]}`
}

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

function buildPathSegments(currentPath) {
  if (!currentPath) return []
  return currentPath
    .split('/')
    .filter(Boolean)
    .map((segment, index, arr) => ({
      label: segment,
      path: arr.slice(0, index + 1).join('/'),
    }))
}

export default function GalleryPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [source, setSource] = useState('cloud')
  const [currentPath, setCurrentPath] = useState('')
  const [files, setFiles] = useState([])
  const [allSDCardFiles, setAllSDCardFiles] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [imagePreview, setImagePreview] = useState(null)
  const [showThumbnails, setShowThumbnails] = useState(false)

  const totalPages = useMemo(() => Math.max(1, Math.ceil(total / PAGE_SIZE)), [total])
  const segments = useMemo(() => buildPathSegments(currentPath), [currentPath])

  const fetchFiles = useCallback(async () => {
    if (!deviceUrl) return
    setLoading(true)
    try {
      if (source === 'sdcard') {
        const data = await getSDCardFiles(deviceUrl, currentPath || 'DCIM')
        const fileArr = Array.isArray(data?.files) ? data.files : []
        fileArr.sort((a, b) => {
          if (a.is_dir && !b.is_dir) return -1
          if (!a.is_dir && b.is_dir) return 1
          return a.name.localeCompare(b.name)
        })
        setAllSDCardFiles(fileArr)
        const start = (page - 1) * PAGE_SIZE
        setFiles(fileArr.slice(start, start + PAGE_SIZE))
        setTotal(fileArr.length)
      } else {
        const data = await getFilesPaginated(deviceUrl, {
          path: currentPath,
          page,
          pageSize: PAGE_SIZE,
        })
        const fileArr = Array.isArray(data?.files) ? data.files : []
        fileArr.sort((a, b) => {
          if (a.is_dir && !b.is_dir) return -1
          if (!a.is_dir && b.is_dir) return 1
          return a.name.localeCompare(b.name)
        })
        setFiles(fileArr)
        setTotal(data?.total ?? fileArr.length)
      }
    } catch (err) {
      toast.error(`Could not load files: ${describeError(err)}`)
      setFiles([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [deviceUrl, currentPath, page, source])

  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  const navigateTo = useCallback((path) => {
    setCurrentPath(path)
    setPage(1)
    setFiles([])
  }, [])

  const handleSourceChange = useCallback((newSource) => {
    setSource(newSource)
    setCurrentPath('')
    setPage(1)
    setFiles([])
  }, [])

  const handleFolderClick = useCallback(
    (folderPath) => {
      navigateTo(folderPath)
    },
    [navigateTo]
  )

  const handleImageClick = useCallback(
    (file) => {
      if (!deviceUrl) return
      const url = source === 'sdcard'
        ? getSDCardPreviewUrl(deviceUrl, file.path)
        : getFileViewUrl(deviceUrl, file.path)
      window.open(url, '_blank', 'noopener,noreferrer')
    },
    [deviceUrl, source]
  )

  const handleBreadcrumbClick = useCallback(
    (index) => {
      if (index === -1) {
        navigateTo('')
      } else {
        navigateTo(segments[index].path)
      }
    },
    [navigateTo, segments]
  )

  const handlePageChange = useCallback((newPage) => {
    setPage(newPage)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }, [])

  const goToPrev = useCallback(() => {
    if (page > 1) handlePageChange(page - 1)
  }, [page, handlePageChange])

  const goToNext = useCallback(() => {
    if (page < totalPages) handlePageChange(page + 1)
  }, [page, totalPages, handlePageChange])

  const visiblePages = useMemo(() => {
    const pages = []
    const start = Math.max(1, page - 2)
    const end = Math.min(totalPages, page + 2)
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }
    return pages
  }, [page, totalPages])

  return (
    <div className="min-h-screen">
      {/* Source toggle */}
      <div className="flex items-center justify-between gap-3 mb-6">
        <div className="flex bg-surface-800 rounded-lg p-1">
          <button
            onClick={() => handleSourceChange('cloud')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
              source === 'cloud'
                ? 'bg-brand-600 text-white shadow'
                : 'text-surface-400 hover:text-surface-200'
            }`}
          >
            <Icon name="cloud" className="w-4 h-4" />
            Cloud
          </button>
          <button
            onClick={() => handleSourceChange('sdcard')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
              source === 'sdcard'
                ? 'bg-brand-600 text-white shadow'
                : 'text-surface-400 hover:text-surface-200'
            }`}
          >
            <Icon name="sd-card" className="w-4 h-4" />
            SD Card
          </button>
        </div>
        {source === 'sdcard' && (
          <button
            onClick={() => setShowThumbnails((enabled) => !enabled)}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
              showThumbnails
                ? 'bg-brand-600 text-white shadow'
                : 'bg-surface-800 text-surface-300 hover:text-surface-100'
            }`}
            aria-pressed={showThumbnails}
          >
            <Icon name="image" className="w-4 h-4" />
            Thumbnails
          </button>
        )}
      </div>

      {/* Full-size image preview overlay */}
      {imagePreview && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
          onClick={() => setImagePreview(null)}
        >
          <button
            className="absolute top-4 right-4 text-white/80 hover:text-white transition-colors"
            onClick={() => setImagePreview(null)}
          >
            <Icon name="x" className="w-8 h-8" />
          </button>
          <img
            src={imagePreview}
            alt="Preview"
            className="max-h-[90vh] max-w-[90vw] object-contain rounded-lg shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}

      {/* Breadcrumb navigation */}
      <nav className="mb-6" aria-label="Breadcrumb">
        <div className="flex items-center gap-1 flex-wrap px-1">
          <button
            onClick={() => handleBreadcrumbClick(-1)}
            className="flex items-center gap-1 px-2 py-1.5 rounded-lg text-sm font-medium transition-colors hover:bg-surface-700/50 text-surface-50 active:scale-[0.97]"
          >
            <Icon name="home" className="w-4 h-4" />
            <span className="hidden sm:inline">Home</span>
          </button>
          {segments.map((seg, idx) => (
            <div key={seg.path} className="flex items-center gap-1">
              <Icon name="chevron-right" className="w-3.5 h-3.5 text-surface-500 flex-shrink-0" />
              <button
                onClick={() => handleBreadcrumbClick(idx)}
                className={`px-2 py-1.5 rounded-lg text-sm font-medium transition-colors active:scale-[0.97] ${
                  idx === segments.length - 1
                    ? 'text-brand-400 bg-brand-400/10 cursor-default'
                    : 'text-surface-300 hover:bg-surface-700/50 hover:text-surface-100'
                }`}
                aria-current={idx === segments.length - 1 ? 'page' : undefined}
              >
                <span className="max-w-[120px] sm:max-w-[200px] truncate inline-block">
                  {seg.label}
                </span>
              </button>
            </div>
          ))}
        </div>
      </nav>

      {/* File listing */}
      {loading ? (
        <div className="flex items-center justify-center py-20">
          <LoadingSpinner size="lg" />
        </div>
      ) : files.length === 0 ? (
        <Card className="text-center py-12">
          <Icon name="folder" className="w-12 h-12 text-surface-500 mx-auto mb-3" />
          <p className="text-surface-400 text-sm">This folder is empty</p>
        </Card>
      ) : (
        <>
          {/* Grid layout */}
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3 sm:gap-4">
            {files.map((file) => (
              <FileCard
                key={file.path}
                file={file}
                deviceUrl={deviceUrl}
                source={source}
                onFolderClick={handleFolderClick}
                onImageClick={handleImageClick}
                onImagePreview={setImagePreview}
                showThumbnail={showThumbnails && source === 'sdcard'}
              />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <nav className="mt-8 flex items-center justify-center gap-1.5" aria-label="Pagination">
              <Button
                variant="ghost"
                size="sm"
                onClick={goToPrev}
                disabled={page <= 1}
                aria-label="Previous page"
              >
                <Icon name="chevron-left" className="w-4 h-4" />
                <span className="hidden sm:inline">Prev</span>
              </Button>

              {visiblePages[0] > 1 && (
                <>
                  <PaginationButton page={1} onClick={handlePageChange} />
                  {visiblePages[0] > 2 && (
                    <span className="px-1 text-surface-500 text-sm">...</span>
                  )}
                </>
              )}

              {visiblePages.map((p) => (
                <PaginationButton
                  key={p}
                  page={p}
                  active={p === page}
                  onClick={handlePageChange}
                />
              ))}

              {visiblePages[visiblePages.length - 1] < totalPages && (
                <>
                  {visiblePages[visiblePages.length - 1] < totalPages - 1 && (
                    <span className="px-1 text-surface-500 text-sm">...</span>
                  )}
                  <PaginationButton page={totalPages} onClick={handlePageChange} />
                </>
              )}

              <Button
                variant="ghost"
                size="sm"
                onClick={goToNext}
                disabled={page >= totalPages}
                aria-label="Next page"
              >
                <span className="hidden sm:inline">Next</span>
                <Icon name="chevron-right" className="w-4 h-4" />
              </Button>

              <span className="ml-3 text-xs text-surface-500 hidden sm:inline">
                {total} {total === 1 ? 'item' : 'items'} total
              </span>
            </nav>
          )}
        </>
      )}
    </div>
  )
}

function PaginationButton({ page: pageNum, active = false, onClick }) {
  return (
    <button
      onClick={() => onClick(pageNum)}
      className={`min-w-[36px] h-9 flex items-center justify-center rounded-lg text-sm font-medium transition-all active:scale-[0.97] ${
        active
          ? 'bg-brand-600 text-white shadow-lg shadow-brand-600/20'
          : 'text-surface-300 hover:bg-surface-700/50'
      }`}
      aria-current={active ? 'page' : undefined}
    >
      {pageNum}
    </button>
  )
}

function FileCard({ file, deviceUrl, source, onFolderClick, onImageClick, onImagePreview, showThumbnail }) {
  const [thumbLoaded, setThumbLoaded] = useState(false)
  const [thumbError, setThumbError] = useState(false)
  const isImg = !file.is_dir && (source === 'sdcard' ? file.is_image : isImageFile(file.name))

  const thumbUrl = getThumbnailUrl(deviceUrl, file.path)
  const previewUrl = source === 'sdcard'
    ? getSDCardPreviewUrl(deviceUrl, file.path)
    : getFileViewUrl(deviceUrl, file.path)

  const handleClick = () => {
    if (file.is_dir) {
      onFolderClick(file.path)
    } else if (isImg) {
      onImageClick(file)
    }
  }

  return (
    <Card
      className={`group relative transition-all duration-200 hover:border-brand-400/30 hover:shadow-lg hover:shadow-brand-400/5 ${
        file.is_dir ? 'cursor-pointer hover:bg-surface-700/40' : 'cursor-pointer'
      }`}
      onClick={handleClick}
    >
      {/* Thumbnail area */}
      <div className="aspect-square rounded-lg overflow-hidden mb-2.5 bg-surface-900/50 flex items-center justify-center relative">
        {isImg && showThumbnail && !thumbError ? (
          <>
            {!thumbLoaded && (
              <div className="absolute inset-0 flex items-center justify-center">
                <LoadingSpinner size="sm" />
              </div>
            )}
            <img
              src={thumbUrl}
              alt={file.name}
              loading="lazy"
              className={`w-full h-full object-cover transition-opacity duration-300 ${
                thumbLoaded ? 'opacity-100' : 'opacity-0'
              }`}
              onLoad={() => setThumbLoaded(true)}
              onError={() => setThumbError(true)}
            />
          </>
        ) : file.is_dir ? (
          <div className="relative">
            <Icon name="folder" className="w-10 h-10 sm:w-12 sm:h-12 text-brand-400/60" />
            <div className="absolute -bottom-1 -right-1 w-5 h-5 rounded-full bg-surface-800 border border-surface-600 flex items-center justify-center">
              <Icon name="chevron-right" className="w-3 h-3 text-surface-400" />
            </div>
          </div>
        ) : (
          <Icon name="image" className="w-10 h-10 sm:w-12 sm:h-12 text-surface-600" />
        )}
      </div>

      {/* File info */}
      <div className="space-y-0.5">
        <p
          className="text-xs sm:text-sm font-medium text-surface-100 truncate"
          title={file.name}
        >
          {file.name}
        </p>
        <div className="flex items-center gap-1.5 text-[10px] sm:text-xs text-surface-400">
          {!file.is_dir && file.size != null && (
            <span>{formatFileSize(file.size)}</span>
          )}
          {!file.is_dir && file.size != null && file.mod_time && (
            <span className="text-surface-600">&middot;</span>
          )}
          {file.mod_time && <span>{formatDate(file.mod_time)}</span>}
        </div>
      </div>

      {/* Image preview button overlay */}
      {isImg && (
        <button
          className="absolute top-2 right-2 w-7 h-7 rounded-full bg-black/60 text-white/80 hover:text-white flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity backdrop-blur-sm"
          onClick={(e) => {
            e.stopPropagation()
            onImagePreview(previewUrl)
          }}
          aria-label={`Preview ${file.name}`}
        >
          <Icon name="magnifying" className="w-3.5 h-3.5" />
        </button>
      )}

      {/* Download button for files */}
      {!file.is_dir && (
        <a
          href={previewUrl}
          download
          onClick={(e) => e.stopPropagation()}
          className="absolute bottom-2 right-2 w-7 h-7 rounded-full bg-black/60 text-white/80 hover:text-white flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity backdrop-blur-sm"
          aria-label={`Download ${file.name}`}
        >
          <Icon name="arrow-down-tray" className="w-3.5 h-3.5" />
        </a>
      )}
    </Card>
  )
}
