[TOC]
## 1. 背景

上周四小伙伴发了Go社区一个帖子下hej8875的回复，如下：

```go
package main
import "fmt"
func main() {
s := []byte("")
s1 := append(s, 'a')
s2 := append(s, 'b')
//fmt.Println(s1, "==========", s2)
fmt.Println(string(s1), "==========", string(s2))
}
// 出现个让我理解不了的现象, 注释时候输出是 b ========== b
// 取消注释输出是 [97] ========== [98] a ========== b 
```

这个回复比原贴有意思，也很有迷惑性。作者测试了下，确实如此，于是就和小伙伴们讨论深究下。开始以为应该挺简单的，理解后，发现涉及挺多知识点，值得跟大家分享下过程。

## 2. slice

### 2.1 内部结构

先抛去注释的这部分```//fmt.Println(s1, "==========", s2)```，后面在讲。 当输出 ```b ========== b```时，已经不符合预期结果：a  b。  我们知道slice内部并不会存储真实的值，而是对数组片段的引用，其结构是:

```go
type slice struct {
    data uintptr
    len int
    cap int
}
```

其data是个指向数组中元素的指针，len是指slice要引用数组中的元素数量。cap是指要引用数组中（从data指向开始计算）剩下的元素数量，这个数量减去len，就是还能向这个slice(array)添加多少元素，如果超出就会发生数据的复制。slice示意图：

```go
s := make([]byte, 5)// 下图
```

![img](https://blog.golang.org/go-slices-usage-and-internals_slice-1.png) 

```go
s = s[2:4]  //下图
```



![img](https://blog.golang.org/go-slices-usage-and-internals_slice-1.png) 

### 2.2 覆盖前值

回到问题上，由此可以推断出，```s := []byte("")``` 其s(slice)实际指向个 byte 的数组。

其capacity 是32，length是 0：

```go
	s := []byte("")
	fmt.Println(cap(s), len(s))
    //输出： 32 0
```

其关键点在于```s1 := append(s, 'a')```中的append，并没有在原slice修改，当然也没办法修改，因为在golang中都是指传递的。当把s传入append函数内时，已经复制一份s1，然后在s1上append ```a```，s1长度是增加了1，但s仍然是0：

```go
s := []byte("")
fmt.Println(cap(s), len(s))
s1 := append(s, 'a')
fmt.Println(cap(s1), len(s1))
fmt.Println(cap(s), len(s))
// 输出
// 32 0
// 32 1
// 32 0
```

由于s，s1是指向同一份数组，所以s1上append ```a```时（即数组[0]=a），等于在s指向数组上的追加，s本身自己不会有任何变化。这也是Go中append的写法都是：

```go
s = append(s,'a')
```

 append会返回s1，重新赋值给s。 如果不赋值的话，s本身记录的数据就滞后了，再次对其追加，就会从滞后的数据开始操作。虽然看起是append，实际上确是把上一次的append给覆盖了。

所以问题的答案是：后append的b，把上次append的a，给覆盖了，这才会输出b b。  

假设底层数组是`arr`，简化成代码：

 ```go
s := []byte("")
s1 := append(s, 'a') // 等同于 arr[0] = 'a'
s2 := append(s, 'b') // 等同于 arr[0] = 'b'
fmt.Println(string(s1), "==========", string(s2)) // 只是把同一份数组打印出来了
 ```

## 3. string

### 3.1 重新分配

老湿，能不能再给力一点？可以，我们继续，先来看个题：

```go
s := []byte{}
s1 := append(s, 'a') 
s2 := append(s, 'b') 
fmt.Println(string(s1), ",", string(s2))
fmt.Println(cap(s), len(s))
```

猜猜输出什么？

答案是：a , b 和 0 0。

上面2.2章节的例子中是：32，0。看来问题关键在这里，两者差别在于一个是默认，另外个是空字符串转的。默认都是0，比较好理解。但为什么容量是32就不符合预期输出了？

因为 capacity 是数组还能添加多少的容量，在能满足的情况，不会重新分配。所以 capacity-length=32，是足够append```a，b```的。我们用make来验证下：

```go
s := make([]byte, 0, 0)
// append 内会重新分配，输出a，b
s := make([]byte, 0, 1)
// append 内不会重新分配，输出b，b
s1 := append(s, 'a')
s2 := append(s, 'b')
fmt.Println(string(s1), ",", string(s2))
```

重新分配指的是：append 会检查slice大小，如果容量不够，会重新创建更大的slice，并把原数组复制一份出来。在```make([]byte,0,0)```这样情况下，s容量肯定不够用，所以s1，s2使用的都是各自从s复制出来的数组，结果也自然符合预期a，b了。

测试重新分配，打印s1:

```go
s := make([]byte, 0, 0)
s1 := append(s, 'a')
fmt.Println(cap(s1), len(s1))
// 输出 8，1。重新分配后扩大了
```

### 3.2 二者转换

那为什么空字符串转的slice的容量是32？而不是0或者8？

只好祭出杀手锏了，翻源码。Go官方提供的有工具，可以查到调用的汇编信息，不然在大片源码中搜索也很累。

```-gcflags``` 是传递参数给Go编译器，```-S -S```是打印汇编调用信息和数据，`-S`只打印调用信息。

```sh
go run -gcflags '-S -S' main.go
```

下面是输出：

```assembly
    0x0000 00000 ()    TEXT    "".main(SB), $264-0
	0x003e 00062 ()   MOVQ    AX, (SP)
	0x0042 00066 ()   XORPS   X0, X0
	0x0045 00069 ()   MOVUPS  X0, 8(SP)
	0x004a 00074 ()   PCDATA  $0, $0
	0x004a 00074 ()   CALL    runtime.stringtoslicebyte(SB)
	0x004f 00079 ()   MOVQ    32(SP), AX
	b , b
```

Go使用的是plan9汇编语法，虽然整体有些不好理解，但也能看出我们需要的关键点：

```assembly
CALL    runtime.stringtoslicebyte(SB)
```

定位源码到`src\runtime\string.go`:

从```stringtoslicebyte```函数中可以看出32的源头，见注释：

```go
const tmpStringBufSize = 32
type tmpBuf [tmpStringBufSize]byte
func stringtoslicebyte(buf *tmpBuf, s string) []byte {
	var b []byte  
	if buf != nil && len(s) <= len(buf) {
		*buf = tmpBuf{}   // tmpBuf的默认容量是32
		b = buf[:len(s)]  // 创建个容量为32，长度为0的新slice，赋值给b。
	} else {
		b = rawbyteslice(len(s))
	}
	copy(b, s)  // s是空字符串，复制过去也是长度0
	return b
}
```

那为什么不是走else中```rawbyteslice```函数？

```go
func rawbyteslice(size int) (b []byte) {
	cap := roundupsize(uintptr(size))
	p := mallocgc(cap, nil, false)
	if cap != uintptr(size) {
		memclrNoHeapPointers(add(p, uintptr(size)), cap-uintptr(size))
	}

	*(*slice)(unsafe.Pointer(&b)) = slice{p, size, int(cap)}
	return
}
```

如果走else的话，容量就不是32了。假如走的话，也不影响得出的结论(覆盖)，比如可以试下：

```go
	s := []byte(strings.Repeat("c", 33))
	s1 := append(s, 'a')
	s2 := append(s, 'b')
	fmt.Println(string(s1), ",", string(s2))
    // cccccccccccccccccccccccccccccccccb , cccccccccccccccccccccccccccccccccb
```

## 4. 逃逸分析

老湿，能不能再给力一点？什么时候该走else？老湿你说了大半天，坑还没填，为啥加上注释就符合预期输出`a，b`?  还有加上注释为啥连容量都变了？

```go
s := []byte("")
fmt.Println(cap(s), len(s))
s1 := append(s, 'a') 
s2 := append(s, 'b') 
fmt.Println(s1, ",", s2)
fmt.Println(string(s1), ",", string(s2))
//输出
// 0 0
// [97] ========== [98]
// a , b
```

如标题，用逃逸分析来解释的话，就比较好理解了。

### 4.1 提高性能

如果一个函数或子程序内有局部对象，返回时返回该对象的指针，那这个指针可能在任何其他地方会被引用，就可以说该指针就成功“逃逸”了 。 而逃逸分析（escape analysis）就是分析这类指针范围的方法，这样做的好处是提高性能：

- 最大的好处应该是减少gc的压力，不逃逸的对象分配在栈上，当函数返回时就回收了资源，不需要gc标记清除。
- 因为逃逸分析完后可以确定哪些变量可以分配在栈上，栈的分配比堆快，性能好
- 同步消除，如果你定义的对象的方法上有同步锁，但在运行时，却只有一个线程在访问，此时逃逸分析后的机器码，会去掉同步锁运行。

Go在编译的时候进行逃逸分析，来决定一个对象放栈上还是放堆上，不逃逸的对象放栈上，可能逃逸的放堆上 。

### 4.2 逃到堆上

取消注释情况下：Go编译程序进行逃逸分析时，检测到```fmt.Println```有引用到s，所以在决定堆上分配s下的数组。在进行string转[]byte时，如果分配到栈上就会有个默认32的容量，分配堆上则没有。

用下面命令执行，可以得到逃逸信息，这个命令只编译程序，上面用的go run -gcflags是传递到编译器并运行程序。

```sh
go tool compile -m main.go
```

取消注释`fmt.Println(s1, ",", s2) `  ([]byte)("")会逃逸到堆上：

 ```shell
main.go:23:13: s1 escapes to heap
main.go:20:13: ([]byte)("") escapes to heap
main.go:23:18: "," escapes to heap
main.go:23:18: s2 escapes to heap
main.go:24:20: string(s1) escapes to heap
main.go:24:20: string(s1) escapes to heap
main.go:24:26: "," escapes to heap
main.go:24:37: string(s2) escapes to heap
main.go:24:37: string(s2) escapes to heap
main.go:23:13: main ... argument does not escape
main.go:24:13: main ... argument does not escape
 ```

加上注释`//fmt.Println(s1, ",", s2) `不会逃逸到堆上：

```sh
go tool compile -m main.go
main.go:24:20: string(s1) escapes to heap
main.go:24:20: string(s1) escapes to heap
main.go:24:26: "," escapes to heap
main.go:24:37: string(s2) escapes to heap
main.go:24:37: string(s2) escapes to heap
main.go:20:13: main ([]byte)("") does not escape
main.go:24:13: main ... argument does not escape
```

###4.3 逃逸分配 

接着继续定位调用`stringtoslicebyte `的地方，在`src\cmd\compile\internal\gc\walk.go` ，注意目录中的`gc`是`Go compiler`的意思，而不是`Garbage Collection `。 为了便于理解，下面代码进行了汇总：

```go
const (
	EscUnknown        = iota
	EscNone           // 结果或参数不逃逸堆上.
 )  
case OSTRARRAYBYTE:
		a := nodnil()   //默认数组为空
		if n.Esc == EscNone {
			// 在堆上为slice创建临时数组
			t := types.NewArray(types.Types[TUINT8], tmpstringbufsize)
			a = nod(OADDR, temp(t), nil)
		}
		n = mkcall("stringtoslicebyte", n.Type, init, a, conv(n.Left, types.Types[TSTRING]))
```

不逃逸情况下会分配个32字节的数组 `t`。逃逸情况下不分配，数组设置为 nil，所以s的容量是0。接着从s上追加a，b到s1，s2，其必然会发生复制，所以不会发生覆盖，也符合预期结果a，b 。再看```stringtoslicebyte```就很清晰了。

```go
func stringtoslicebyte(buf *tmpBuf, s string) []byte {
	var b []byte
	if buf != nil && len(s) <= len(buf) { 
		*buf = tmpBuf{}
		b = buf[:len(s)]
	} else {
		b = rawbyteslice(len(s))
	}
	copy(b, s)
	return b
}
```

### 4.4 大小分配 

不逃逸情况下默认32。那逃逸情况下分配策略是？

```go
s := []byte("a")
fmt.Println(cap(s))
s1 := append(s, 'a')
s2 := append(s, 'b')
fmt.Print(s1, s2)
```

如果是空字符串它的cap是0。”a“字符串时是 8。

大小取决于```src\runtime\size.go``` 中的roundupsize 函数和 class_to_size 变量。

这些大小增加的因子，是由  ```src\runtime\mksizeclasses.go ```生成的。

## 5. 版本差异

老湿，能不能再给力一点？ 老湿你讲的全是错误的，我跑的结果和你是反的。对，你没错，作者也没错，毕竟我们程序用Go写程序，如果Go底层发生变化了，肯定结果不一样。作者在调研过程中，发现另外博客得到```stringtoslicebyte```源码是：

```go
func stringtoslicebyte(s String) (b Slice) {
    b.array = runtime·mallocgc(s.len, 0, FlagNoScan|FlagNoZero);
    b.len = s.len;
    b.cap = s.len;
    runtime·memmove(b.array, s.str, s.len);
}
```

继续翻旧版代码，到1.3.2版是这样:

```go
func stringtoslicebyte(s String) (b Slice) {
	uintptr cap;
	cap = runtime·roundupsize(s.len);
	b.array = runtime·mallocgc(cap, 0, FlagNoScan|FlagNoZero);
	b.len = s.len;
	b.cap = cap;
	runtime·memmove(b.array, s.str, s.len);
	if(cap != b.len)
		runtime·memclr(b.array+b.len, cap-b.len);
}
```

1.6.4版:

```go
func stringtoslicebyte(buf *tmpBuf, s string) []byte {
	var b []byte
	if buf != nil && len(s) <= len(buf) {
		b = buf[:len(s):len(s)]
	} else {
		b = rawbyteslice(len(s))
	}
	copy(b, s)
	return b
}
```

更古老的：

```c
struct __go_open_array
__go_string_to_byte_array (String str)
{
  uintptr cap;
  unsigned char *data;
  struct __go_open_array ret;

  cap = runtime_roundupsize (str.len);
  data = (unsigned char *) runtime_mallocgc (cap, 0, FlagNoScan | FlagNoZero);
  __builtin_memcpy (data, str.str, str.len);
  if (cap != (uintptr) str.len)
    __builtin_memset (data + str.len, 0, cap - (uintptr) str.len);
  ret.__values = (void *) data;
  ret.__count = str.len;
  ret.__capacity = str.len;
  return ret;
}
```

作者在1.6.4版本上测试， 得到的结果确实是反的，注释了反而得到预期结果 a, b。  本文中使用的是1.10.2

## 6. 结论

老湿，能不能再给力一点？🐶，再继续一天时间都没了。 不过大家可以猜猜不逃逸情况下，为什么要给个32字节大小的默认数组？

总结下：

1. 注释时输出b，b。是因为没有逃逸，所以分配了默认32字节大小的数组，2次append都是在数组[0]赋值。后值覆盖前值，所以才是b，b。
2. 取消注释时输出a，b。是因为`fmt.Println`引用了s，逃逸分析时需要逃逸并且是空字符串，所以分配了空数组，2次append都是重新分配新的slice，所以输出a，b。

另外这种写法是没意义的，也是极不推荐的。应该把 []byte(“string”)，当成只读的来用，不然就容易出现难排查的bug。

### 6.1 参考

原帖是：https://gocn.io/question/1852

https://gocn.io/article/355

https://go-review.googlesource.com/c/gofrontend/+/30827

http://golang-examples.tumblr.com/post/86403044869/conversion-between-byte-and-string-dont-share