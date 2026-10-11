package cn.iyque.phase1;
import cn.iyque.domain.IYQueCustomerInfo;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.autoconfigure.domain.EntityScan;
import org.springframework.data.jpa.repository.config.EnableJpaRepositories;
import org.springframework.scheduling.annotation.EnableScheduling;

@SpringBootApplication
@EntityScan(basePackageClasses={CustomerApplication.class,IYQueCustomerInfo.class})
@EnableJpaRepositories(considerNestedRepositories=true)
@EnableScheduling
public class CustomerApplication {
 public static void main(String[] args) { SpringApplication.run(CustomerApplication.class,args); }
}

