website-operator编排文件样例：
```yaml
apiVersion: website.xianyuluo.com/v1
kind: Website
metadata:
  name: nginx-app
  namespace: xxx
spec:
  size: 2
  image: xianyuluo/nginx:1.12.2.nginx-example-1
```

```yaml
apiVersion: app.example.com/v1
kind: AppService
metadata:
  name: nginx-app
spec:
  size: 2
  image: nginx:1.7.9
  ports:
    - port: 80
      targetPort: 80
      nodePort: 30002
```