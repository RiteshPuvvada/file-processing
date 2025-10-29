
# 🧪 File Processing Worker (Golang)

This project implements a **Concurrent File Processing Worker** in Go that scans an input directory, processes files in parallel, computes MD5 hashes, and writes atomic logs safely.  
It demonstrates clean code, concurrency, and filesystem atomicity principles — ideal for interviews or assessments.

---

## 🚀 Overview

The worker scans an input folder (`./input/`) for subdirectories named like `r_<id>/` (e.g., `r_001/`).  
Each subfolder’s files are processed **concurrently**, and results are logged in JSON format.

After processing:
- The folder is renamed to `d_<id>/` if **all files succeed**
- Or renamed to `f_<id>/` if **any file fails**

---

## 📂 Example Folder Flow

**Before:**
```
input/
└── r_001/
    ├── file1.txt
    ├── file2.pdf
```

**After successful processing:**
```
input/
└── d_001/
    ├── file1.txt
    ├── file2.pdf
    ├── log.json
```

**If a file fails:**
```
input/
└── f_001/
    ├── file1.txt
    ├── broken.pdf
    ├── log.json
```

---

## ⚙️ Features

- 🧵 **Concurrent file processing** with configurable worker limits  
- 💾 **Atomic logging** using `log.tmp` → `log.json` pattern  
- 🧩 **Thread-safe operations** via goroutines and channels  
- 🧠 **Graceful error handling** per file  
- 🪶 Uses **only Go standard library** (no dependencies)

---

## 💻 How to Run

### 1️⃣ Clone or copy this project
```bash
git clone https://github.com/RiteshPuvvada/file-processing.git
cd file-processing
```

### 2️⃣ Create an input folder with test files
```bash
mkdir -p input/r_001
echo "hello world" > input/r_001/file1.txt
echo "go concurrency test" > input/r_001/file2.txt
```

### 3️⃣ Build the program
```bash
go build -o worker ./main.go
```

### 4️⃣ Run the worker
```bash
./worker -input ./input -concurrency 4 -v
```

✅ The program will:
- Process all files inside each `r_*` folder
- Create a `log.json` file per folder
- Rename folder to `d_<id>` (success) or `f_<id>` (failure)

---

## ⚡ Command Line Options

| Flag | Description | Default |
|------|--------------|----------|
| `-input` | Path to the input directory | `./input` |
| `-concurrency` | Number of concurrent file processors | Number of CPU cores |
| `-v` | Enable verbose logging | Off |

Example:
```bash
./worker -input ./input -concurrency 8 -v
```

---

## 📘 Example log.json

```json
[
  {
    "filename": "file1.txt",
    "status": "success",
    "md5": "4d186321c1a7f0f354b297e8914ab240",
    "timestamp": "2025-04-16T15:22:10Z"
  },
  {
    "filename": "broken.pdf",
    "status": "error",
    "error": "failed to read file: permission denied",
    "timestamp": "2025-04-16T15:22:12Z"
  }
]
```

---

## 🧠 Design Notes

- Uses `sync.WaitGroup` + semaphore (`chan struct{}`) to limit concurrent processing.
- Atomic file operations via `os.Rename` for both logs and folder renaming.
- `io.Copy()` streams files to avoid high memory usage.
- Deterministic logs sorted by filename.

---

## 🧩 Tech Stack

- **Language:** Go 1.25+
- **Libraries:** Only Go standard library  
- **Platform:** Cross-platform (macOS, Linux, Windows)

---

## 👨‍💻 Author
Ritesh Puvvada
