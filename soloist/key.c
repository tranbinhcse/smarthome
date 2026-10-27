// soloist-key.so: Soloist chỉ nhận khoá API qua tham số -k, mà dòng lệnh thì mọi tiến trình trên máy đọc được ở
// /proc/<pid>/cmdline. hubd chạy Soloist với LD_PRELOAD trỏ tới thư viện này, khoá trong biến môi trường SOLOIST_API_KEY
// (/proc/<pid>/environ chỉ root đọc được) và tham số giữ chỗ "-k @SOLOIST_API_KEY". Hàm dưới chạy trước main() của Soloist:
// glibc truyền cho nó đúng mảng argv sẽ đưa vào main(), nên chỉ đổi con trỏ trong mảng: vùng nhớ mà /proc/<pid>/cmdline
// đọc vẫn là chữ giữ chỗ. Xoá biến khỏi môi trường để tiến trình con của Soloist (crashpad-handler) không thừa hưởng khoá.
#include <stdlib.h>
#include <string.h>

__attribute__((constructor)) static void soloist_key(int argc, char **argv, char **envp) {
	(void)envp;
	const char *k = getenv("SOLOIST_API_KEY");
	if (!k || !*k)
		return;
	for (int i = 1; i < argc; i++)
		if (strcmp(argv[i], "@SOLOIST_API_KEY") == 0)
			argv[i] = strdup(k);
	unsetenv("SOLOIST_API_KEY");
}
